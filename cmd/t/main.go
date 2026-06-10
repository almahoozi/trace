package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/almahoozi/trace/internal/app"
	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
	"github.com/almahoozi/trace/internal/grafana"
	"github.com/almahoozi/trace/internal/platform"
	"github.com/almahoozi/trace/internal/secrets"
	"github.com/almahoozi/trace/internal/tui"
)

var (
	commit  string
	ref     string
	version string
)

func main() {
	var (
		showVersion bool
		configPath  string
	)

	flag.BoolVar(&showVersion, "v", false, "print version information and exit")
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.StringVar(&configPath, "config", "", "config file path (defaults to platform config dir)")
	flag.Parse()

	if showVersion {
		printBuildInfo()
		return
	}

	args := flag.Args()
	if len(args) >= 1 && args[0] == "config" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "edit") {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		path, err := config.EnsureFile(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to prepare config file: %v\n", err)
			os.Exit(1)
		}
		if err := platform.OpenInEditor(path); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open config in editor: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	mode, err := resolveMode(args, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		printUsage()
		os.Exit(1)
	}

	if strings.TrimSpace(cfg.Grafana.BaseURL) == "" {
		if err := promptAndSaveBaseURL(&cfg); err != nil {
			fmt.Fprintf(os.Stderr, "failed to set grafana.base_url: %v\n", err)
			os.Exit(1)
		}
	}
	secretStore := secrets.NewStore(cfg)

	token, err := secretStore.LoadToken(cfg)
	if err != nil {
		token, err = promptAndSaveToken(cfg, secretStore)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve auth token: %v\n", err)
			os.Exit(1)
		}
	}

	httpClient := grafana.NewHTTPClient(cfg.Grafana.Timeout())
	grafanaClient := grafana.NewClient(cfg.Grafana.BaseURL, token, httpClient)
	fetcher := app.NewFetcher(grafanaClient)

	if mode.isBrowse {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
		items, err := fetcher.FetchTraceList(ctx, cfg, mode.environment, mode.query, 50)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to fetch trace list: %v\n", err)
			os.Exit(1)
		}

		program := tea.NewProgram(
			tui.NewBrowseModel(
				cfg,
				mode.environment,
				mode.query,
				items,
				func(ctx context.Context, traceID string) (*domain.Session, error) {
					return fetcher.FetchTraceSessionInEnvironment(ctx, cfg, mode.environment, traceID)
				},
				func(ctx context.Context) ([]domain.TraceListItem, error) {
					return fetcher.FetchTraceList(ctx, cfg, mode.environment, mode.query, 50)
				},
				platform.OpenURL,
			),
			tea.WithAltScreen(),
		)
		finalModel, err := program.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
			os.Exit(1)
		}
		browseModel, ok := finalModel.(tui.BrowseModel)
		if ok {
			if session := browseModel.LastSession(); session != nil {
				summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
				if err != nil {
					fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
				}
				if strings.TrimSpace(summary) != "" {
					fmt.Fprintln(os.Stdout, summary)
				}
			}
		}
		return
	}

	traceID := mode.traceID

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
	defer cancel()

	session, err := fetcher.FetchTraceSession(ctx, cfg, traceID)
	if err != nil {
		if errors.Is(err, app.ErrTraceNotFound) {
			fmt.Fprintf(os.Stderr, "trace %q not found in configured environments\n", traceID)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "failed to fetch trace session: %v\n", err)
		os.Exit(1)
	}

	program := tea.NewProgram(tui.NewModel(cfg, session, platform.OpenURL), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
		os.Exit(1)
	}

	summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
	if err != nil {
		fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
	}
	if strings.TrimSpace(summary) != "" {
		fmt.Fprintln(os.Stdout, summary)
	}
}

type cliMode struct {
	isBrowse    bool
	environment string
	query       string
	traceID     string
}

func resolveMode(args []string, cfg config.Config) (cliMode, error) {
	if len(args) == 0 {
		return cliMode{}, fmt.Errorf("missing command arguments")
	}

	if env := lookupEnvironment(cfg, args[0]); env != "" {
		return cliMode{
			isBrowse:    true,
			environment: env,
			query:       strings.TrimSpace(strings.Join(args[1:], " ")),
		}, nil
	}

	if len(args) == 1 {
		return cliMode{traceID: strings.TrimSpace(args[0])}, nil
	}

	return cliMode{}, fmt.Errorf("invalid command")
}

func lookupEnvironment(cfg config.Config, candidate string) string {
	target := strings.TrimSpace(candidate)
	for _, env := range cfg.Environments {
		if strings.EqualFold(strings.TrimSpace(env.Name), target) {
			return env.Name
		}
	}
	return ""
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-v|--version] [--config path] <trace-id>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] <env> [query]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] config\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] config edit\n", os.Args[0])
}

func shouldColorizeStdout() bool {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	return true
}

func printBuildInfo() {
	if commit == "" && ref == "" && version == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "build: %s %s %s\n", commit, ref, version)
}

func promptAndSaveBaseURL(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Grafana base URL is required before continuing.")
	fmt.Fprint(os.Stderr, "Enter Grafana base URL (e.g. https://grafana.example.com): ")
	input, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	baseURL := strings.TrimSpace(input)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return fmt.Errorf("empty value")
	}
	cfg.Grafana.BaseURL = baseURL
	return config.Save(*cfg)
}

func promptAndSaveToken(cfg config.Config, store secrets.Store) (string, error) {
	tokenPath, err := store.TokenLocation(cfg)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Grafana token is required before continuing.")
	fmt.Fprintf(os.Stderr, "Token will be saved to %s (mode 600).\n", tokenPath)
	fmt.Fprint(os.Stderr, "Enter Grafana token: ")
	input, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(input))
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	if err := store.SaveToken(cfg, token); err != nil {
		return "", err
	}
	return token, nil
}

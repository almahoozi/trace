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
	if len(args) == 1 && args[0] == "config" {
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

	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: %s [-v|--version] [--config path] <trace-id>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       %s [--config path] config\n", os.Args[0])
		os.Exit(1)
	}
	traceID := args[0]

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
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

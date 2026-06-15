package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/almahoozi/trace/internal/app"
	"github.com/almahoozi/trace/internal/config"
	"github.com/almahoozi/trace/internal/domain"
	"github.com/almahoozi/trace/internal/grafana"
	"github.com/almahoozi/trace/internal/platform"
	"github.com/almahoozi/trace/internal/runlog"
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
		forceFetch  bool
	)

	flag.BoolVar(&showVersion, "v", false, "print version information and exit")
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.BoolVar(&forceFetch, "f", false, "force network fetch instead of cache")
	flag.BoolVar(&forceFetch, "force", false, "force network fetch instead of cache")
	flag.StringVar(&configPath, "config", "", "config file path (defaults to platform config dir)")
	flag.Parse()
	args := flag.Args()

	if len(args) >= 1 && args[0] == "logs" {
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		path, err := config.RunLogPath(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve run log file: %v\n", err)
			os.Exit(1)
		}
		if err := ensureRunLogFile(path); err != nil {
			fmt.Fprintf(os.Stderr, "failed to prepare run log file: %v\n", err)
			os.Exit(1)
		}
		if err := platform.OpenInEditor(path); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open run log in editor: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "caches" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "clear") {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		dir, err := app.SnapshotCacheDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve cache directory: %v\n", err)
			os.Exit(1)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "failed to prepare cache directory: %v\n", err)
			os.Exit(1)
		}
		if len(args) == 2 && args[1] == "clear" {
			if err := os.RemoveAll(dir); err != nil {
				fmt.Fprintf(os.Stderr, "failed to clear cache directory: %v\n", err)
				os.Exit(1)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "failed to recreate cache directory: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stdout, "cleared caches in %s\n", dir)
			return
		}
		if err := platform.OpenInEditor(dir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to open cache directory: %v\n", err)
			os.Exit(1)
		}
		return
	}

	initRunLog(configPath)
	defer runlog.Close()
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	runlog.Info("cli args parsed", "argv", os.Args[1:], "show_version", showVersion, "arg_count", len(args))

	if showVersion {
		runlog.Info("printing version information")
		printBuildInfo()
		return
	}

	if len(args) >= 1 && args[0] == "config" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "edit") {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		if len(args) == 2 && args[1] == "edit" {
			path, err := config.EnsureFile(configPath)
			if err != nil {
				runlog.Error("failed to ensure config file", "error", err, "config_path_flag", configPath)
				fmt.Fprintf(os.Stderr, "failed to prepare config file: %v\n", err)
				os.Exit(1)
			}
			if err := platform.OpenInEditor(path); err != nil {
				runlog.Error("failed to open config in editor", "error", err, "config_path", path)
				fmt.Fprintf(os.Stderr, "failed to open config in editor: %v\n", err)
				os.Exit(1)
			}
			runlog.Info("opened config in editor", "config_path", path)
			return
		}

		cfg, err := config.Load(configPath)
		if err != nil {
			runlog.Error("failed to load config for in-app editor", "error", err, "config_path_flag", configPath)
			fmt.Fprintf(os.Stderr, "failed to load config for in-app mode: %v\n", err)
			fmt.Fprintf(os.Stderr, "tip: use '%s config edit' to open the raw file\n", os.Args[0])
			os.Exit(1)
		}
		configModel := tui.NewConfigModel(cfg)
		program := tea.NewProgram(configModel, tea.WithAltScreen())
		if _, err := program.Run(); err != nil {
			runlog.Error("config tui failed", "error", err)
			fmt.Fprintf(os.Stderr, "config tui failed: %v\n", err)
			os.Exit(1)
		}
		runlog.Info("opened config in app mode", "config_path", cfg.Path)
		return
	}
	if len(args) >= 1 && args[0] == "open" {
		if len(args) != 2 {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		cfg, err := loadConfigForOffline(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
			os.Exit(1)
		}
		snapshotPath, err := app.ResolveSnapshotOpenPath(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to resolve snapshot path: %v\n", err)
			os.Exit(1)
		}
		session, err := app.LoadSessionSnapshot(snapshotPath)
		if err != nil {
			runlog.Error("failed to load session snapshot", "error", err, "path", snapshotPath)
			fmt.Fprintf(os.Stderr, "failed to load snapshot: %v\n", err)
			os.Exit(1)
		}
		program := tea.NewProgram(tui.NewModel(cfg, session, platform.OpenURL, defaultSnapshotSaver), tea.WithAltScreen())
		if _, err := program.Run(); err != nil {
			runlog.Error("trace tui failed for snapshot", "error", err)
			fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
			os.Exit(1)
		}
		summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
		if err != nil {
			runlog.Warn("trace summary render warning", "error", err)
			fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
		}
		if strings.TrimSpace(summary) != "" {
			fmt.Fprintln(os.Stdout, summary)
		}
		return
	}
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		runlog.Error("failed to load config", "error", err, "config_path_flag", configPath)
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}
	runlog.Info("config loaded", "config_path", cfg.Path, "environment_count", len(cfg.Environments), "grafana_timeout_seconds", cfg.Grafana.TimeoutSeconds)

	isExportMode := len(args) >= 1 && args[0] == "export"
	mode := cliMode{}
	if !isExportMode {
		mode, err = resolveMode(args, cfg)
		if err != nil {
			runlog.Warn("invalid command arguments", "error", err, "argv", args)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			printUsage()
			os.Exit(1)
		}
		runlog.Info("mode resolved", "browse", mode.isBrowse, "environment", mode.environment, "trace_id", mode.traceID, "query", mode.query, "trace_in_env_only", mode.traceInEnvOnly)
	}

	if !isExportMode && !forceFetch && mode.traceID != "" {
		traceID := strings.TrimSpace(mode.traceID)
		if snapshotPath, err := app.ResolveSnapshotOpenPath(traceID); err == nil {
			session, err := app.LoadSessionSnapshot(snapshotPath)
			if err == nil {
				if mode.traceInEnvOnly && !strings.EqualFold(strings.TrimSpace(mode.environment), strings.TrimSpace(session.Environment)) {
					runlog.Info("snapshot environment mismatch; skipping cache", "trace_id", traceID, "requested_environment", mode.environment, "snapshot_environment", session.Environment)
				} else {
					runlog.Info("loaded trace session from snapshot cache", "trace_id", traceID, "snapshot_path", snapshotPath)
					program := tea.NewProgram(tui.NewModel(cfg, session, platform.OpenURL, defaultSnapshotSaver), tea.WithAltScreen())
					if _, err := program.Run(); err != nil {
						runlog.Error("trace tui failed", "error", err)
						fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
						os.Exit(1)
					}
					summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
					if err != nil {
						runlog.Warn("trace summary render warning", "error", err)
						fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
					}
					if strings.TrimSpace(summary) != "" {
						fmt.Fprintln(os.Stdout, summary)
					}
					return
				}
			}
		}
	}

	if strings.TrimSpace(cfg.Grafana.BaseURL) == "" {
		runlog.Warn("grafana base_url missing; prompting user")
		if err := promptAndSaveBaseURL(&cfg); err != nil {
			runlog.Error("failed to set grafana base_url", "error", err)
			fmt.Fprintf(os.Stderr, "failed to set grafana.base_url: %v\n", err)
			os.Exit(1)
		}
		runlog.Info("grafana base_url saved")
	}
	secretStore := secrets.NewStore(cfg)

	token, err := secretStore.LoadToken(cfg)
	if err != nil {
		runlog.Warn("token not available from env/keyring; prompting user", "error", err)
		token, err = promptAndSaveToken(cfg, secretStore)
		if err != nil {
			runlog.Error("failed to resolve auth token", "error", err)
			fmt.Fprintf(os.Stderr, "failed to resolve auth token: %v\n", err)
			os.Exit(1)
		}
		runlog.Info("token saved to configured location")
	}

	httpClient := grafana.NewHTTPClient(cfg.Grafana.Timeout())
	grafanaClient := grafana.NewClient(cfg.Grafana.BaseURL, token, httpClient)
	fetcher := app.NewFetcher(grafanaClient)

	if len(args) >= 1 && args[0] == "export" {
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintf(os.Stderr, "invalid command\n")
			printUsage()
			os.Exit(1)
		}
		traceID := strings.TrimSpace(args[1])
		if traceID == "" {
			fmt.Fprintf(os.Stderr, "trace id is required\n")
			os.Exit(1)
		}
		outPath := ""
		if len(args) == 3 {
			outPath = strings.TrimSpace(args[2])
		}
		if outPath == "" {
			var err error
			outPath, err = app.DefaultSnapshotPath(traceID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to build default snapshot path: %v\n", err)
				os.Exit(1)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
		session, err := fetcher.FetchTraceSession(ctx, cfg, traceID)
		cancel()
		if err != nil {
			if errors.Is(err, app.ErrTraceNotFound) {
				fmt.Fprintf(os.Stderr, "trace %q not found in configured environments\n", traceID)
				os.Exit(2)
			}
			fmt.Fprintf(os.Stderr, "failed to fetch trace session: %v\n", err)
			os.Exit(1)
		}
		if err := app.SaveSessionSnapshot(outPath, session); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save snapshot: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "saved snapshot to %s\n", outPath)
		return
	}

	if mode.isBrowse {
		runlog.Info("browse mode started", "environment", mode.environment, "query", mode.query)
		statusLabel := "fetching"
		if strings.TrimSpace(mode.query) != "" {
			statusLabel = "querying"
		}
		status := startProgressStatus(fmt.Sprintf("%s traces in %s", statusLabel, mode.environment))
		defer status.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
		items, err := fetcher.FetchTraceList(ctx, cfg, mode.environment, mode.query, 50)
		cancel()
		status.Stop()
		if err != nil {
			runlog.Error("failed to fetch trace list", "error", err, "environment", mode.environment)
			fmt.Fprintf(os.Stderr, "failed to fetch trace list: %v\n", err)
			os.Exit(1)
		}
		runlog.Info("browse list fetched", "environment", mode.environment, "trace_count", len(items))

		program := tea.NewProgram(
			tui.NewBrowseModel(
				cfg,
				mode.environment,
				mode.query,
				items,
				func(ctx context.Context, traceID string) (*domain.Session, error) {
					session, err := fetcher.FetchTraceSessionInEnvironment(ctx, cfg, mode.environment, traceID)
					if err != nil {
						return nil, err
					}
					if err := autoExportSnapshotOnOpen(cleanupCtx, cfg, session); err != nil {
						runlog.Warn("auto-export snapshot failed", "error", err, "trace_id", traceID)
					}
					return session, nil
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
			runlog.Error("browse tui failed", "error", err)
			fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
			os.Exit(1)
		}
		browseModel, ok := finalModel.(tui.BrowseModel)
		if ok {
			if session := browseModel.LastSession(); session != nil {
				summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
				if err != nil {
					runlog.Warn("trace summary render warning", "error", err)
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
	runlog.Info("trace mode started", "trace_id", traceID, "environment", mode.environment, "trace_in_env_only", mode.traceInEnvOnly)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
	status := startProgressStatus("searching trace")
	defer status.Stop()

	var session *domain.Session
	if mode.traceInEnvOnly {
		status.Update(fmt.Sprintf("searching trace in %s", mode.environment))
		prefetchCtx, prefetchCancel := context.WithCancel(context.Background())
		defer prefetchCancel()
		prefetchResultCh := make(chan logsFetchResult, 1)
		prefetchRangeStart, prefetchRangeEnd := sinceWindow(cfg.Logs.Since, time.Now())
		go func() {
			logsCtx, logsCancel := context.WithTimeout(prefetchCtx, time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
			defer logsCancel()
			logs, logsErr := fetcher.FetchLogsForEnvironment(logsCtx, cfg, mode.environment, traceID, time.Time{}, time.Time{}, 0)
			prefetchResultCh <- logsFetchResult{entries: logs, err: logsErr}
		}()

		session, err = fetcher.FindTraceSessionInEnvironment(ctx, cfg, mode.environment, traceID)

		logsPadding := 30 * time.Second
		logsLoader := func(ctx context.Context) ([]domain.LogEntry, error) {
			wantedStart, wantedEnd := traceWindowWithPadding(session, logsPadding)
			if windowsOverlap(prefetchRangeStart, prefetchRangeEnd, wantedStart, wantedEnd) {
				select {
				case prefetched := <-prefetchResultCh:
					if prefetched.err == nil {
						return filterLogsByWindow(prefetched.entries, wantedStart, wantedEnd), nil
					}
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			} else {
				prefetchCancel()
			}

			logsCtx, logsCancel := context.WithTimeout(ctx, time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
			defer logsCancel()
			return fetcher.FetchLogsForSession(logsCtx, cfg, session, logsPadding)
		}
		logsReady := func(updated *domain.Session) {
			if err := autoExportSnapshotOnOpen(cleanupCtx, cfg, updated); err != nil {
				runlog.Warn("auto-export snapshot failed", "error", err, "trace_id", traceID)
			}
		}

		if err == nil {
			spanCount := 0
			if session.Trace != nil {
				spanCount = session.Trace.SpanCount
			}
			runlog.Info("trace session fetched", "trace_id", traceID, "environment", session.Environment, "span_count", spanCount)

			cancel()
			status.Stop()

			program := tea.NewProgram(tui.NewModelWithDeferredLogs(cfg, session, platform.OpenURL, defaultSnapshotSaver, logsLoader, logsReady), tea.WithAltScreen())
			if _, err := program.Run(); err != nil {
				runlog.Error("trace tui failed", "error", err)
				fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
				os.Exit(1)
			}

			summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
			if err != nil {
				runlog.Warn("trace summary render warning", "error", err)
				fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
			}
			if strings.TrimSpace(summary) != "" {
				fmt.Fprintln(os.Stdout, summary)
			}
			return
		}
	} else {
		status.Update(formatEnvSearchStatus(nil, cfg.Environments, "searching trace"))
		var (
			obsMu       sync.Mutex
			states      = map[string]string{}
			matchLocked bool
		)
		session, err = fetcher.FindTraceSession(ctx, cfg, traceID, func(event app.TraceLookupEvent) {
			obsMu.Lock()
			defer obsMu.Unlock()
			if matchLocked {
				return
			}
			switch event.Stage {
			case "querying":
				states[event.Environment] = "querying"
				status.Update(formatEnvSearchStatus(states, cfg.Environments, "searching trace"))
			case "not_found":
				states[event.Environment] = "not_found"
				status.Update(formatEnvSearchStatus(states, cfg.Environments, "searching trace"))
			case "error":
				states[event.Environment] = "error"
				status.Update(formatEnvSearchStatus(states, cfg.Environments, "searching trace"))
			case "matched":
				matchLocked = true
				states[event.Environment] = "matched"
				status.Update(formatEnvSearchStatus(states, cfg.Environments, fmt.Sprintf("trace found in %s", event.Environment)))
			}
		})
	}
	cancel()
	status.Stop()
	if err != nil {
		if errors.Is(err, app.ErrTraceNotFound) {
			runlog.Warn("trace not found", "trace_id", traceID)
			if mode.traceInEnvOnly {
				fmt.Fprintf(os.Stderr, "trace %q not found in environment %q\n", traceID, mode.environment)
			} else {
				fmt.Fprintf(os.Stderr, "trace %q not found in configured environments\n", traceID)
			}
			os.Exit(2)
		}
		runlog.Error("failed to fetch trace session", "error", err, "trace_id", traceID)
		fmt.Fprintf(os.Stderr, "failed to fetch trace session: %v\n", err)
		os.Exit(1)
	}
	spanCount := 0
	if session.Trace != nil {
		spanCount = session.Trace.SpanCount
	}
	runlog.Info("trace session fetched", "trace_id", traceID, "environment", session.Environment, "span_count", spanCount)

	logsPadding := 30 * time.Second
	logsLoader := func(ctx context.Context) ([]domain.LogEntry, error) {
		logsCtx, logsCancel := context.WithTimeout(ctx, time.Duration(cfg.Grafana.TimeoutSeconds+10)*time.Second)
		defer logsCancel()
		return fetcher.FetchLogsForSession(logsCtx, cfg, session, logsPadding)
	}
	logsReady := func(updated *domain.Session) {
		if err := autoExportSnapshotOnOpen(cleanupCtx, cfg, updated); err != nil {
			runlog.Warn("auto-export snapshot failed", "error", err, "trace_id", traceID)
		}
	}

	program := tea.NewProgram(tui.NewModelWithDeferredLogs(cfg, session, platform.OpenURL, defaultSnapshotSaver, logsLoader, logsReady), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		runlog.Error("trace tui failed", "error", err)
		fmt.Fprintf(os.Stderr, "tui failed: %v\n", err)
		os.Exit(1)
	}

	summary, err := app.RenderTraceSummaryWithColor(cfg, session, shouldColorizeStdout())
	if err != nil {
		runlog.Warn("trace summary render warning", "error", err)
		fmt.Fprintf(os.Stderr, "trace summary warning: %v\n", err)
	}
	if strings.TrimSpace(summary) != "" {
		fmt.Fprintln(os.Stdout, summary)
	}
}

type cliMode struct {
	isBrowse       bool
	environment    string
	query          string
	traceID        string
	traceInEnvOnly bool
}

func resolveMode(args []string, cfg config.Config) (cliMode, error) {
	if len(args) == 0 {
		return cliMode{}, fmt.Errorf("missing command arguments")
	}

	if env := lookupEnvironment(cfg, args[0]); env != "" {
		if len(args) == 2 && looksLikeTraceID(args[1]) {
			return cliMode{
				environment:    env,
				traceID:        strings.TrimSpace(args[1]),
				traceInEnvOnly: true,
			}, nil
		}
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

var traceIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{16,64}$`)

func looksLikeTraceID(value string) bool {
	return traceIDPattern.MatchString(strings.TrimSpace(value))
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-v|--version] [--config path] <trace-id>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [-f|--force] [--config path] <trace-id>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [-f|--force] [--config path] <env> <trace-id>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] <env> [query]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] export <trace-id> [file]\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] open <file>\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] caches\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] caches clear\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] config\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] config edit\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "       %s [--config path] logs\n", os.Args[0])
}

func loadConfigForOffline(configPath string) (config.Config, error) {
	if cfg, err := config.Load(configPath); err == nil {
		return cfg, nil
	}
	return config.DefaultConfig(), nil
}

func defaultSnapshotSaver(session *domain.Session) (string, error) {
	if session == nil || session.Trace == nil {
		return "", fmt.Errorf("nil session")
	}
	path, err := app.DefaultSnapshotPath(session.Trace.TraceID)
	if err != nil {
		return "", err
	}
	if err := app.SaveSessionSnapshot(path, session); err != nil {
		return "", err
	}
	return path, nil
}

var snapshotCleanupState struct {
	mu      sync.Mutex
	running bool
}

func autoExportSnapshotOnOpen(ctx context.Context, cfg config.Config, session *domain.Session) error {
	if !cfg.Cache.AutoExportOnOpen {
		return nil
	}
	if session == nil || session.Trace == nil {
		return nil
	}
	path, err := app.DefaultSnapshotPath(session.Trace.TraceID)
	if err != nil {
		return err
	}
	if err := app.SaveSessionSnapshot(path, session); err != nil {
		return err
	}
	startSnapshotCleanupWorker(ctx, cfg)
	runlog.Info("auto-exported snapshot on open", "trace_id", session.Trace.TraceID, "snapshot_path", path)
	return nil
}

func startSnapshotCleanupWorker(ctx context.Context, cfg config.Config) {
	snapshotCleanupState.mu.Lock()
	if snapshotCleanupState.running {
		snapshotCleanupState.mu.Unlock()
		return
	}
	snapshotCleanupState.running = true
	snapshotCleanupState.mu.Unlock()

	maxBytes := cfg.Cache.MaxSizeBytes()
	targetBytes := cfg.Cache.CleanupTargetBytes()
	go func() {
		defer func() {
			snapshotCleanupState.mu.Lock()
			snapshotCleanupState.running = false
			snapshotCleanupState.mu.Unlock()
		}()
		err := app.CleanupSnapshotCache(ctx, maxBytes, targetBytes)
		if err != nil && !errors.Is(err, context.Canceled) {
			runlog.Warn("snapshot cache cleanup failed", "error", err)
		}
	}()
}

func initRunLog(configPath string) {
	path, err := config.RunLogPath(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to resolve run log path: %v\n", err)
		return
	}
	if err := runlog.Start(path); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to initialize run log: %v\n", err)
		return
	}
	runlog.Info("run log initialized", "log_path", path)
}

func ensureRunLogFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
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

type logsFetchResult struct {
	entries []domain.LogEntry
	err     error
}

func traceWindowWithPadding(session *domain.Session, padding time.Duration) (time.Time, time.Time) {
	if session == nil || session.Trace == nil {
		return time.Time{}, time.Time{}
	}
	start := session.Trace.StartTime
	end := session.Trace.StartTime.Add(session.Trace.Duration)
	if end.Before(start) {
		start, end = end, start
	}
	if padding > 0 {
		start = start.Add(-padding)
		end = end.Add(padding)
	}
	return start, end
}

func sinceWindow(since string, now time.Time) (time.Time, time.Time) {
	end := now
	d, err := time.ParseDuration("-" + strings.TrimSpace(since))
	if err != nil {
		d = -60 * time.Minute
	}
	start := end.Add(d)
	if end.Before(start) {
		start, end = end, start
	}
	return start, end
}

func windowsOverlap(aStart, aEnd, bStart, bEnd time.Time) bool {
	if aStart.IsZero() || aEnd.IsZero() || bStart.IsZero() || bEnd.IsZero() {
		return false
	}
	if aEnd.Before(aStart) {
		aStart, aEnd = aEnd, aStart
	}
	if bEnd.Before(bStart) {
		bStart, bEnd = bEnd, bStart
	}
	return !aEnd.Before(bStart) && !bEnd.Before(aStart)
}

func filterLogsByWindow(entries []domain.LogEntry, start, end time.Time) []domain.LogEntry {
	if start.IsZero() || end.IsZero() {
		return entries
	}
	filtered := make([]domain.LogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Timestamp.IsZero() {
			continue
		}
		if !entry.Timestamp.Before(start) && !entry.Timestamp.After(end) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func formatEnvSearchStatus(states map[string]string, envs []config.Environment, prefix string) string {
	ordered := orderedEnvironmentNames(envs)
	parts := make([]string, 0, len(ordered))
	for _, envName := range ordered {
		stage := "pending"
		if states != nil {
			if value, ok := states[envName]; ok && strings.TrimSpace(value) != "" {
				stage = value
			}
		}
		switch stage {
		case "querying":
			parts = append(parts, envName+":searching")
		case "not_found":
			parts = append(parts, envName+":miss")
		case "error":
			parts = append(parts, envName+":error")
		case "matched":
			parts = append(parts, envName+":match")
		default:
			parts = append(parts, envName+":pending")
		}
	}
	if len(parts) == 0 {
		return prefix
	}
	return prefix + " | " + strings.Join(parts, " ")
}

func orderedEnvironmentNames(envs []config.Environment) []string {
	rank := map[string]int{"dev": 0, "stg": 1, "prd": 2}
	names := make([]string, 0, len(envs))
	for _, env := range envs {
		name := strings.TrimSpace(env.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		li := strings.ToLower(names[i])
		lj := strings.ToLower(names[j])
		ri, iok := rank[li]
		rj, jok := rank[lj]
		if iok && jok {
			return ri < rj
		}
		if iok != jok {
			return iok
		}
		return li < lj
	})
	return names
}

type progressStatus struct {
	mu      sync.Mutex
	message string
	done    chan struct{}
	wg      sync.WaitGroup
	enabled bool
}

func startProgressStatus(initial string) *progressStatus {
	s := &progressStatus{
		message: initial,
		done:    make(chan struct{}),
		enabled: term.IsTerminal(int(os.Stderr.Fd())),
	}
	if !s.enabled {
		return s
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		frames := []string{"-", "\\", "|", "/"}
		i := 0
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.mu.Lock()
				msg := s.message
				s.mu.Unlock()
				fmt.Fprintf(os.Stderr, "\r%s %s", frames[i%len(frames)], msg)
				i++
			}
		}
	}()
	return s
}

func (s *progressStatus) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

func (s *progressStatus) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.done:
		return
	default:
		close(s.done)
	}
	s.wg.Wait()
	if s.enabled {
		fmt.Fprint(os.Stderr, "\r\033[K")
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
	fmt.Fprintf(os.Stderr, "Token will be saved to %s.\n", tokenPath)
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

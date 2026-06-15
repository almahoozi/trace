package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const appName = "trace"
const runLogFileName = "run.log"

type Config struct {
	Path         string        `json:"-"`
	ConfigDir    string        `json:"-"`
	Grafana      GrafanaConfig `json:"grafana"`
	Auth         AuthConfig    `json:"auth"`
	Cache        CacheConfig   `json:"cache"`
	Environments []Environment `json:"environments"`
	Logs         LogsConfig    `json:"logs"`
	URLs         URLConfig     `json:"urls"`
	Output       OutputConfig  `json:"output"`
	UI           UIConfig      `json:"ui"`
	Keymap       KeymapConfig  `json:"keymap"`
}

type GrafanaConfig struct {
	BaseURL        string `json:"base_url"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (g GrafanaConfig) Timeout() time.Duration {
	if g.TimeoutSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(g.TimeoutSeconds) * time.Second
}

type AuthConfig struct {
	TokenEnv  string `json:"token_env"`
	TokenFile string `json:"token_file"`
}

type CacheConfig struct {
	AutoExportOnOpen bool `json:"auto_export_on_open"`
}

type Environment struct {
	Name             string `json:"name"`
	TempoDatasource  string `json:"tempo_datasource_uid"`
	LokiDatasource   string `json:"loki_datasource_uid"`
	LogQueryTemplate string `json:"log_query_template"`
	BetterstackID    string `json:"betterstack_source_id"`
}

type LogsConfig struct {
	Since          string   `json:"since"`
	Limit          int      `json:"limit"`
	LevelThreshold string   `json:"level_threshold"`
	LevelOrder     []string `json:"level_order"`
	MessageField   string   `json:"message_field"`
	ServiceField   string   `json:"service_field"`
	LevelField     string   `json:"level_field"`
	TimestampField string   `json:"timestamp_field"`
}

type URLConfig struct {
	GrafanaTraceTemplate   string `json:"grafana_trace_template"`
	BetterstackLogTemplate string `json:"betterstack_log_template"`
}

type OutputConfig struct {
	TraceSummaryTemplate string `json:"trace_summary_template"`
}

const LegacyTraceSummaryTemplate = `{{bright "["}}{{gray .environment}}{{bright "]"}}{{if .has_errors}}{{red "!"}}{{end}}{{if .http_status_code}} {{http_status_color .http_status_code .http_status_code}}{{end}} {{bright .operation}} {{bright "("}}{{duration_color .duration .duration_display}}{{bright ")"}} {{bright "-"}} {{bright "["}}{{gray .start_display}}{{bright " - "}}{{gray .end_display}}{{bright "]"}}`

const DefaultTraceSummaryTemplate = `{{bright "["}}{{gray .environment}}{{bright "]"}}{{if .trace_id}}{{bright .trace_id}}{{end}}{{if .has_errors}}{{red "!"}}{{end}}{{if .http_status_code}} {{http_status_color .http_status_code .http_status_code}}{{end}} {{bright .operation}} {{bright "("}}{{duration_color .duration .duration_display}}{{bright ")"}} {{bright "-"}} {{bright "["}}{{gray .start_display}}{{bright " - "}}{{gray .end_display}}{{bright "]"}}`

type UIConfig struct {
	LogColumns              []string          `json:"log_columns"`
	LogDetailParts          []string          `json:"log_detail_parts"`
	TraceDetailParts        []string          `json:"trace_detail_parts"`
	SpanIcons               map[string]string `json:"span_icons"`
	AdditionalServiceColors []string          `json:"additional_service_colors"`
	ServiceMap              ServiceMapConfig  `json:"service_map"`
	Timezone                string            `json:"timezone"`
	SectionOrder            []string          `json:"section_order"`
	CollapsedSections       []string          `json:"collapsed_sections"`
	DefaultFullscreen       bool              `json:"default_fullscreen"`
	FocusSection            string            `json:"focus_section"`
}

type ServiceMapConfig struct {
	DependencyAliases   map[string]string    `json:"dependency_aliases"`
	DependencyTypes     map[string]string    `json:"dependency_types"`
	DependencyTypeRules []DependencyTypeRule `json:"dependency_type_rules"`
	ServiceColor        string               `json:"service_color"`
	SidecarColor        string               `json:"sidecar_color"`
	ExternalColor       string               `json:"external_color"`
	TypeColors          map[string]string    `json:"type_colors"`
}

type DependencyTypeRule struct {
	Match string `json:"match"`
	Type  string `json:"type"`
}

type KeymapConfig struct {
	Global map[string][]string `json:"global"`
	Trace  map[string][]string `json:"trace"`
	Logs   map[string][]string `json:"logs"`
	JSON   map[string][]string `json:"json"`
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "config.json"), nil
}

func RunLogPath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		path, err := DefaultPath()
		if err != nil {
			return "", err
		}
		configPath = path
	}
	return filepath.Join(filepath.Dir(filepath.Clean(configPath)), runLogFileName), nil
}

func EnsureFile(path string) (string, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return "", err
		}
	}

	path = filepath.Clean(path)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := DefaultConfig()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		buf, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, append(buf, '\n'), 0o644); err != nil {
			return "", err
		}
		return path, nil
	} else if err != nil {
		return "", err
	}

	return path, nil
}

func Load(path string) (Config, error) {
	var err error
	if path == "" {
		path, err = DefaultPath()
		if err != nil {
			return Config{}, err
		}
	}

	path, err = EnsureFile(path)
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config json: %w", err)
	}
	cfg.Path = path
	cfg.ConfigDir = filepath.Dir(path)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if len(c.Environments) == 0 {
		return errors.New("at least one environment is required")
	}
	for _, env := range c.Environments {
		if env.Name == "" || env.TempoDatasource == "" || env.LokiDatasource == "" {
			return fmt.Errorf("invalid environment %+v", env)
		}
	}
	if _, err := c.DisplayLocation(); err != nil {
		return err
	}
	return nil
}

func (c Config) DisplayLocation() (*time.Location, error) {
	raw := strings.TrimSpace(c.UI.Timezone)
	if raw == "" || strings.EqualFold(raw, "local") {
		return time.Local, nil
	}
	if strings.EqualFold(raw, "utc") {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid ui.timezone %q: %w", raw, err)
	}
	return loc, nil
}

func Save(cfg Config) error {
	path := cfg.Path
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(buf, '\n'), 0o644)
}

func ResolveToken(cfg Config) (string, error) {
	envKey := cfg.Auth.TokenEnv
	if envKey == "" {
		envKey = "TRACE_GRAFANA_TOKEN"
	}
	if token, ok := os.LookupEnv(envKey); ok && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), nil
	}

	tokenFile, err := TokenFilePath(cfg)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("set %s or create token file %s", envKey, tokenFile)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("empty token file %s", tokenFile)
	}
	return token, nil
}

func TokenFilePath(cfg Config) (string, error) {
	tokenFile := cfg.Auth.TokenFile
	if tokenFile == "" {
		tokenFile = "token"
	}
	if filepath.IsAbs(tokenFile) {
		return tokenFile, nil
	}
	configDir := cfg.ConfigDir
	if configDir == "" {
		path := cfg.Path
		if path == "" {
			var err error
			path, err = DefaultPath()
			if err != nil {
				return "", err
			}
		}
		configDir = filepath.Dir(path)
	}
	return filepath.Join(configDir, tokenFile), nil
}

func SaveToken(cfg Config, token string) error {
	tokenFile, err := TokenFilePath(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token is empty")
	}
	return os.WriteFile(tokenFile, []byte(token+"\n"), 0o600)
}

func DefaultConfig() Config {
	return Config{
		Grafana: GrafanaConfig{
			BaseURL:        "",
			TimeoutSeconds: 15,
		},
		Auth: AuthConfig{
			TokenEnv:  "TRACE_GRAFANA_TOKEN",
			TokenFile: "token",
		},
		Cache: CacheConfig{
			AutoExportOnOpen: true,
		},
		Environments: []Environment{
			{
				Name:             "dev",
				TempoDatasource:  "traces-dev",
				LokiDatasource:   "logs-dev",
				LogQueryTemplate: `{k8s_namespace=~".+"} |~ ` + "`\"trace[-_]id\":\"{{trace_id}}\"`" + ` | json`,
				BetterstackID:    "772925",
			},
			{
				Name:             "stg",
				TempoDatasource:  "traces-stg",
				LokiDatasource:   "logs-stg",
				LogQueryTemplate: `{k8s_namespace=~".+"} |~ ` + "`\"trace[-_]id\":\"{{trace_id}}\"`" + ` | json`,
				BetterstackID:    "773432",
			},
			{
				Name:             "prd",
				TempoDatasource:  "traces-prd",
				LokiDatasource:   "logs-prd",
				LogQueryTemplate: `{k8s_namespace=~".+"} |~ ` + "`\"trace[-_]id\":\"{{trace_id}}\"`" + ` | json`,
				BetterstackID:    "775784",
			},
		},
		Logs: LogsConfig{
			Since:          "60m",
			Limit:          500,
			LevelThreshold: "debug",
			LevelOrder:     []string{"trace", "debug", "info", "warn", "error", "fatal"},
			MessageField:   "message",
			ServiceField:   "service",
			LevelField:     "level",
			TimestampField: "timestamp",
		},
		URLs: URLConfig{
			GrafanaTraceTemplate:   "",
			BetterstackLogTemplate: "https://telemetry.betterstack.com/team/t000000/tail?q={{trace_id}}&s={{betterstack_source_id}}&rf=now-60m",
		},
		Output: OutputConfig{
			TraceSummaryTemplate: DefaultTraceSummaryTemplate,
		},
		UI: UIConfig{
			LogColumns:              []string{"timestamp", "service", "level", "message"},
			LogDetailParts:          []string{"log", "labels", "raw"},
			TraceDetailParts:        []string{"metadata", "attributes", "events", "links"},
			SpanIcons:               map[string]string{"server": "[srv]", "client": "[cli]", "producer": "[prd]", "consumer": "[con]", "internal": "[int]"},
			AdditionalServiceColors: []string{},
			ServiceMap: ServiceMapConfig{
				DependencyAliases:   map[string]string{},
				DependencyTypes:     map[string]string{},
				DependencyTypeRules: []DependencyTypeRule{},
				ServiceColor:        "68",
				SidecarColor:        "244",
				ExternalColor:       "214",
				TypeColors:          map[string]string{"db": "39", "third_party": "178"},
			},
			Timezone:          "local",
			SectionOrder:      []string{"trace", "service_map", "logs"},
			CollapsedSections: []string{},
			DefaultFullscreen: false,
			FocusSection:      "trace",
		},
		Keymap: KeymapConfig{
			Global: map[string][]string{
				"quit":              {"q", "ctrl+c"},
				"help":              {"?"},
				"export_snapshot":   {"E"},
				"back":              {"esc"},
				"switch_tab":        {"tab"},
				"switch_tab_back":   {"shift+tab"},
				"toggle_fullscreen": {"f"},
				"toggle_collapse":   {"c"},
			},
			Trace: map[string][]string{
				"up":            {"k", "up"},
				"down":          {"j", "down"},
				"expand":        {"l", "right"},
				"collapse":      {"h", "left"},
				"toggle":        {"space"},
				"details":       {"enter"},
				"open_external": {"o"},
			},
			Logs: map[string][]string{
				"up":            {"k", "up"},
				"down":          {"j", "down"},
				"details":       {"enter"},
				"level_up":      {"+"},
				"level_down":    {"-"},
				"open_external": {"o"},
			},
			JSON: map[string][]string{
				"up":       {"k", "up"},
				"down":     {"j", "down"},
				"expand":   {"l", "right"},
				"collapse": {"h", "left"},
				"toggle":   {"enter"},
				"back":     {"esc"},
			},
		},
	}
}

func PlatformConfigLocationHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "~/Library/Application Support/trace/config.json"
	case "windows":
		return "%AppData%\\trace\\config.json"
	default:
		return "~/.config/trace/config.json"
	}
}

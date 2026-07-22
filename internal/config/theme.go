package config

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

const ThemeEnvVar = "TRACE_THEME"

const (
	ThemeColorTitle                = "ui.title"
	ThemeColorMuted                = "ui.muted"
	ThemeColorSectionBorder        = "ui.section.border"
	ThemeColorSectionBorderActive  = "ui.section.border_active"
	ThemeColorTableBandA           = "ui.table.band_a"
	ThemeColorTableBandB           = "ui.table.band_b"
	ThemeColorTableVisual          = "ui.table.visual"
	ThemeColorTableCursor          = "ui.table.cursor"
	ThemeColorTableCursorVisual    = "ui.table.cursor_visual"
	ThemeColorSummaryBright        = "ui.summary.bright"
	ThemeColorSummaryGray          = "ui.summary.gray"
	ThemeColorSummarySuccess       = "ui.summary.success"
	ThemeColorSummaryInfo          = "ui.summary.info"
	ThemeColorSummaryWarn          = "ui.summary.warn"
	ThemeColorSummaryError         = "ui.summary.error"
	ThemeColorServiceFallback      = "ui.service.fallback"
	ThemeColorServiceMapService    = "ui.service_map.service"
	ThemeColorServiceMapSidecar    = "ui.service_map.sidecar"
	ThemeColorServiceMapExternal   = "ui.service_map.external"
	ThemeColorServiceMapCost       = "ui.service_map.cost"
	ThemeColorServiceMapTypeDB     = "ui.service_map.type.db"
	ThemeColorServiceMapType3rd    = "ui.service_map.type.third_party"
	ThemeColorLogLevelError        = "ui.log_level.error"
	ThemeColorLogLevelWarn         = "ui.log_level.warn"
	ThemeColorLogLevelInfo         = "ui.log_level.info"
	ThemeColorLogLevelDebug        = "ui.log_level.debug"
	ThemeColorLogLevelTrace        = "ui.log_level.trace"
	ThemeColorLogLevelDefault      = "ui.log_level.default"
	ThemeColorANSISummaryGray      = "ansi.summary.gray"
	ThemeColorANSISummaryLight     = "ansi.summary.light"
	ThemeColorANSISummaryBright    = "ansi.summary.bright"
	ThemeColorANSISummaryRed       = "ansi.summary.red"
	ThemeColorANSIDurationFast     = "ansi.duration.fast"
	ThemeColorANSIDurationNormal   = "ansi.duration.normal"
	ThemeColorANSIDurationSlow     = "ansi.duration.slow"
	ThemeColorANSIDurationVerySlow = "ansi.duration.very_slow"
	ThemeColorANSIHTTPDefault      = "ansi.http.default"
	ThemeColorANSIHTTPSuccess      = "ansi.http.success"
	ThemeColorANSIHTTPRedirect     = "ansi.http.redirect"
	ThemeColorANSIHTTPClientError  = "ansi.http.client_error"
	ThemeColorANSIHTTPServerError  = "ansi.http.server_error"
)

type ThemeMap map[string]ThemePalette

type ThemePalette struct {
	Colors         map[string]string `json:"colors"`
	ServicePalette []string          `json:"service_palette"`
}

type ResolvedTheme struct {
	Name    string
	Palette ThemePalette
	FromEnv bool
}

func DefaultThemes() ThemeMap {
	dark := ThemePalette{
		Colors: map[string]string{
			ThemeColorTitle:                "12",
			ThemeColorMuted:                "241",
			ThemeColorSectionBorder:        "240",
			ThemeColorSectionBorderActive:  "33",
			ThemeColorTableBandA:           "234",
			ThemeColorTableBandB:           "235",
			ThemeColorTableVisual:          "236",
			ThemeColorTableCursor:          "238",
			ThemeColorTableCursorVisual:    "61",
			ThemeColorSummaryBright:        "15",
			ThemeColorSummaryGray:          "245",
			ThemeColorSummarySuccess:       "2",
			ThemeColorSummaryInfo:          "4",
			ThemeColorSummaryWarn:          "3",
			ThemeColorSummaryError:         "1",
			ThemeColorServiceFallback:      "244",
			ThemeColorServiceMapService:    "68",
			ThemeColorServiceMapSidecar:    "244",
			ThemeColorServiceMapExternal:   "214",
			ThemeColorServiceMapCost:       "250",
			ThemeColorServiceMapTypeDB:     "39",
			ThemeColorServiceMapType3rd:    "178",
			ThemeColorLogLevelError:        "196",
			ThemeColorLogLevelWarn:         "214",
			ThemeColorLogLevelInfo:         "39",
			ThemeColorLogLevelDebug:        "111",
			ThemeColorLogLevelTrace:        "244",
			ThemeColorLogLevelDefault:      "250",
			ThemeColorANSISummaryGray:      "90",
			ThemeColorANSISummaryLight:     "37",
			ThemeColorANSISummaryBright:    "97",
			ThemeColorANSISummaryRed:       "31",
			ThemeColorANSIDurationFast:     "32",
			ThemeColorANSIDurationNormal:   "97",
			ThemeColorANSIDurationSlow:     "33",
			ThemeColorANSIDurationVerySlow: "31",
			ThemeColorANSIHTTPDefault:      "97",
			ThemeColorANSIHTTPSuccess:      "32",
			ThemeColorANSIHTTPRedirect:     "34",
			ThemeColorANSIHTTPClientError:  "33",
			ThemeColorANSIHTTPServerError:  "31",
		},
		ServicePalette: []string{"68", "173", "71", "176", "74", "179", "109", "175", "75", "181"},
	}

	light := ThemePalette{
		Colors: map[string]string{
			ThemeColorTitle:                "18",
			ThemeColorMuted:                "242",
			ThemeColorSectionBorder:        "248",
			ThemeColorSectionBorderActive:  "24",
			ThemeColorTableBandA:           "255",
			ThemeColorTableBandB:           "254",
			ThemeColorTableVisual:          "153",
			ThemeColorTableCursor:          "189",
			ThemeColorTableCursorVisual:    "111",
			ThemeColorSummaryBright:        "16",
			ThemeColorSummaryGray:          "241",
			ThemeColorSummarySuccess:       "28",
			ThemeColorSummaryInfo:          "25",
			ThemeColorSummaryWarn:          "166",
			ThemeColorSummaryError:         "124",
			ThemeColorServiceFallback:      "242",
			ThemeColorServiceMapService:    "24",
			ThemeColorServiceMapSidecar:    "244",
			ThemeColorServiceMapExternal:   "166",
			ThemeColorServiceMapCost:       "240",
			ThemeColorServiceMapTypeDB:     "25",
			ThemeColorServiceMapType3rd:    "130",
			ThemeColorLogLevelError:        "160",
			ThemeColorLogLevelWarn:         "166",
			ThemeColorLogLevelInfo:         "25",
			ThemeColorLogLevelDebug:        "62",
			ThemeColorLogLevelTrace:        "244",
			ThemeColorLogLevelDefault:      "238",
			ThemeColorANSISummaryGray:      "90",
			ThemeColorANSISummaryLight:     "30",
			ThemeColorANSISummaryBright:    "30",
			ThemeColorANSISummaryRed:       "31",
			ThemeColorANSIDurationFast:     "32",
			ThemeColorANSIDurationNormal:   "30",
			ThemeColorANSIDurationSlow:     "33",
			ThemeColorANSIDurationVerySlow: "31",
			ThemeColorANSIHTTPDefault:      "30",
			ThemeColorANSIHTTPSuccess:      "32",
			ThemeColorANSIHTTPRedirect:     "34",
			ThemeColorANSIHTTPClientError:  "33",
			ThemeColorANSIHTTPServerError:  "31",
		},
		ServicePalette: []string{"24", "25", "26", "27", "31", "32", "33", "62", "94", "130"},
	}

	return ThemeMap{
		"default": cloneThemePalette(dark),
		"dark":    cloneThemePalette(dark),
		"light":   cloneThemePalette(light),
	}
}

func (c Config) ResolveTheme() ResolvedTheme {
	name := strings.TrimSpace(c.Theme)
	fromEnv := false
	if envTheme, ok := os.LookupEnv(ThemeEnvVar); ok && strings.TrimSpace(envTheme) != "" {
		name = strings.TrimSpace(envTheme)
		fromEnv = true
	}
	if name == "" {
		name = "dark"
	}

	themes := cloneThemeMap(DefaultThemes())
	for key, value := range c.Themes {
		themes[key] = cloneThemePalette(value)
	}

	selected, found := lookupTheme(themes, name)
	if !found {
		name = "dark"
		selected, _ = lookupTheme(themes, name)
	}
	base, _ := lookupTheme(themes, "default")
	resolved := mergeThemePalette(base, selected)
	resolved = normalizeThemePalette(resolved)

	return ResolvedTheme{Name: name, Palette: resolved, FromEnv: fromEnv}
}

func (c Config) ThemeExists(name string) bool {
	_, ok := lookupTheme(c.ThemeCatalog(), name)
	return ok
}

func (c Config) ThemeCatalog() ThemeMap {
	themes := cloneThemeMap(DefaultThemes())
	for key, value := range c.Themes {
		themes[key] = cloneThemePalette(value)
	}
	return themes
}

func (c Config) ThemeNames() []string {
	catalog := c.ThemeCatalog()
	names := make([]string, 0, len(catalog))
	for key := range catalog {
		if strings.TrimSpace(key) != "" {
			names = append(names, key)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func (c Config) LookupTheme(name string) (string, ThemePalette, bool) {
	catalog := c.ThemeCatalog()
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ThemePalette{}, false
	}
	for key, value := range catalog {
		if strings.EqualFold(strings.TrimSpace(key), trimmed) {
			return key, value, true
		}
	}
	return "", ThemePalette{}, false
}

func IsBuiltInTheme(name string) bool {
	_, ok := lookupTheme(DefaultThemes(), name)
	return ok
}

func (c Config) SetTheme(name string, palette ThemePalette) Config {
	updated := c
	if updated.Themes == nil {
		updated.Themes = ThemeMap{}
	}
	updated.Themes[name] = normalizeThemePalette(cloneThemePalette(palette))
	return updated
}

func normalizeThemePalette(palette ThemePalette) ThemePalette {
	palette.Colors = cloneColorMap(palette.Colors)
	if palette.ServicePalette == nil {
		palette.ServicePalette = []string{}
	}
	trimmed := make([]string, 0, len(palette.ServicePalette))
	for _, raw := range palette.ServicePalette {
		if color := strings.TrimSpace(raw); color != "" {
			trimmed = append(trimmed, color)
		}
	}
	palette.ServicePalette = trimmed
	return palette
}

func NormalizeThemePalette(palette ThemePalette) ThemePalette {
	return normalizeThemePalette(palette)
}

func mergeThemePalette(base ThemePalette, overlay ThemePalette) ThemePalette {
	merged := normalizeThemePalette(base)
	if merged.Colors == nil {
		merged.Colors = map[string]string{}
	}
	for key, value := range overlay.Colors {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			merged.Colors[key] = trimmed
		}
	}
	if len(overlay.ServicePalette) > 0 {
		merged.ServicePalette = append([]string{}, overlay.ServicePalette...)
	}
	return normalizeThemePalette(merged)
}

func cloneThemeMap(in ThemeMap) ThemeMap {
	cloned := ThemeMap{}
	for key, palette := range in {
		cloned[key] = cloneThemePalette(palette)
	}
	return cloned
}

func cloneThemePalette(in ThemePalette) ThemePalette {
	out := ThemePalette{Colors: cloneColorMap(in.Colors)}
	if len(in.ServicePalette) > 0 {
		out.ServicePalette = append([]string{}, in.ServicePalette...)
	} else {
		out.ServicePalette = []string{}
	}
	return out
}

func cloneColorMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out[key] = trimmed
		}
	}
	return out
}

func lookupTheme(themes ThemeMap, name string) (ThemePalette, bool) {
	target := strings.TrimSpace(name)
	if target == "" {
		return ThemePalette{}, false
	}
	for key, value := range themes {
		if strings.EqualFold(strings.TrimSpace(key), target) {
			return value, true
		}
	}
	return ThemePalette{}, false
}

func ANSIColorCode(color string, fallback string) string {
	trimmed := strings.TrimSpace(color)
	if trimmed == "" {
		trimmed = strings.TrimSpace(fallback)
	}
	if trimmed == "" {
		return ""
	}
	if _, err := strconv.Atoi(trimmed); err == nil {
		return "38;5;" + trimmed
	}
	return trimmed
}

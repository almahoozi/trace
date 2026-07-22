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

const (
	ThemePalleteTextFocussed     = "text_focussed"
	ThemePalleteTextUnfocussed   = "text_unfocussed"
	ThemePalleteBorder           = "border"
	ThemePalleteBorderActive     = "border_active"
	ThemePalleteSurfaceA         = "surface_a"
	ThemePalleteSurfaceB         = "surface_b"
	ThemePalleteSurfaceVisual    = "surface_visual"
	ThemePalleteSurfaceCursor    = "surface_cursor"
	ThemePalleteSurfaceCursorVis = "surface_cursor_visual"
	ThemePalleteSuccess          = "success"
	ThemePalleteInfo             = "info"
	ThemePalleteWarning          = "warning"
	ThemePalleteError            = "error"
	ThemePalleteServicePrimary   = "service_primary"
	ThemePalleteServiceSidecar   = "service_sidecar"
	ThemePalleteServiceExternal  = "service_external"
	ThemePalleteServiceCost      = "service_cost"
	ThemePalleteServiceDB        = "service_db"
	ThemePalleteService3rd       = "service_third_party"
)

var colorPalleteFallback = map[string]string{
	ThemeColorTitle:                ThemePalleteTextFocussed,
	ThemeColorMuted:                ThemePalleteTextUnfocussed,
	ThemeColorSectionBorder:        ThemePalleteBorder,
	ThemeColorSectionBorderActive:  ThemePalleteBorderActive,
	ThemeColorTableBandA:           ThemePalleteSurfaceA,
	ThemeColorTableBandB:           ThemePalleteSurfaceB,
	ThemeColorTableVisual:          ThemePalleteSurfaceVisual,
	ThemeColorTableCursor:          ThemePalleteSurfaceCursor,
	ThemeColorTableCursorVisual:    ThemePalleteSurfaceCursorVis,
	ThemeColorSummaryBright:        ThemePalleteTextFocussed,
	ThemeColorSummaryGray:          ThemePalleteTextUnfocussed,
	ThemeColorSummarySuccess:       ThemePalleteSuccess,
	ThemeColorSummaryInfo:          ThemePalleteInfo,
	ThemeColorSummaryWarn:          ThemePalleteWarning,
	ThemeColorSummaryError:         ThemePalleteError,
	ThemeColorServiceFallback:      ThemePalleteTextUnfocussed,
	ThemeColorServiceMapService:    ThemePalleteServicePrimary,
	ThemeColorServiceMapSidecar:    ThemePalleteServiceSidecar,
	ThemeColorServiceMapExternal:   ThemePalleteServiceExternal,
	ThemeColorServiceMapCost:       ThemePalleteServiceCost,
	ThemeColorServiceMapTypeDB:     ThemePalleteServiceDB,
	ThemeColorServiceMapType3rd:    ThemePalleteService3rd,
	ThemeColorLogLevelError:        ThemePalleteError,
	ThemeColorLogLevelWarn:         ThemePalleteWarning,
	ThemeColorLogLevelInfo:         ThemePalleteInfo,
	ThemeColorLogLevelDebug:        ThemePalleteTextUnfocussed,
	ThemeColorLogLevelTrace:        ThemePalleteTextUnfocussed,
	ThemeColorLogLevelDefault:      ThemePalleteTextFocussed,
	ThemeColorANSISummaryGray:      ThemePalleteTextUnfocussed,
	ThemeColorANSISummaryLight:     ThemePalleteTextFocussed,
	ThemeColorANSISummaryBright:    ThemePalleteTextFocussed,
	ThemeColorANSISummaryRed:       ThemePalleteError,
	ThemeColorANSIDurationFast:     ThemePalleteSuccess,
	ThemeColorANSIDurationNormal:   ThemePalleteTextFocussed,
	ThemeColorANSIDurationSlow:     ThemePalleteWarning,
	ThemeColorANSIDurationVerySlow: ThemePalleteError,
	ThemeColorANSIHTTPDefault:      ThemePalleteTextFocussed,
	ThemeColorANSIHTTPSuccess:      ThemePalleteSuccess,
	ThemeColorANSIHTTPRedirect:     ThemePalleteInfo,
	ThemeColorANSIHTTPClientError:  ThemePalleteWarning,
	ThemeColorANSIHTTPServerError:  ThemePalleteError,
}

type ThemeMap map[string]ThemePalette

type ThemePalette struct {
	Colors         map[string]string `json:"colors"`
	Pallete        map[string]string `json:"pallete"`
	ServicePalette []string          `json:"service_palette"`
}

var themeColorKeys = []string{
	ThemeColorTitle,
	ThemeColorMuted,
	ThemeColorSectionBorder,
	ThemeColorSectionBorderActive,
	ThemeColorTableBandA,
	ThemeColorTableBandB,
	ThemeColorTableVisual,
	ThemeColorTableCursor,
	ThemeColorTableCursorVisual,
	ThemeColorSummaryBright,
	ThemeColorSummaryGray,
	ThemeColorSummarySuccess,
	ThemeColorSummaryInfo,
	ThemeColorSummaryWarn,
	ThemeColorSummaryError,
	ThemeColorServiceFallback,
	ThemeColorServiceMapService,
	ThemeColorServiceMapSidecar,
	ThemeColorServiceMapExternal,
	ThemeColorServiceMapCost,
	ThemeColorServiceMapTypeDB,
	ThemeColorServiceMapType3rd,
	ThemeColorLogLevelError,
	ThemeColorLogLevelWarn,
	ThemeColorLogLevelInfo,
	ThemeColorLogLevelDebug,
	ThemeColorLogLevelTrace,
	ThemeColorLogLevelDefault,
	ThemeColorANSISummaryGray,
	ThemeColorANSISummaryLight,
	ThemeColorANSISummaryBright,
	ThemeColorANSISummaryRed,
	ThemeColorANSIDurationFast,
	ThemeColorANSIDurationNormal,
	ThemeColorANSIDurationSlow,
	ThemeColorANSIDurationVerySlow,
	ThemeColorANSIHTTPDefault,
	ThemeColorANSIHTTPSuccess,
	ThemeColorANSIHTTPRedirect,
	ThemeColorANSIHTTPClientError,
	ThemeColorANSIHTTPServerError,
}

type ResolvedTheme struct {
	Name     string
	Palette  ThemePalette
	Selected ThemePalette
	Default  ThemePalette
	FromEnv  bool
}

func DefaultThemes() ThemeMap {
	dark := ThemePalette{
		Colors: map[string]string{},
		Pallete: map[string]string{
			ThemePalleteTextFocussed:     "15",
			ThemePalleteTextUnfocussed:   "245",
			ThemePalleteBorder:           "240",
			ThemePalleteBorderActive:     "33",
			ThemePalleteSurfaceA:         "234",
			ThemePalleteSurfaceB:         "235",
			ThemePalleteSurfaceVisual:    "236",
			ThemePalleteSurfaceCursor:    "238",
			ThemePalleteSurfaceCursorVis: "61",
			ThemePalleteSuccess:          "2",
			ThemePalleteInfo:             "4",
			ThemePalleteWarning:          "3",
			ThemePalleteError:            "1",
			ThemePalleteServicePrimary:   "68",
			ThemePalleteServiceSidecar:   "244",
			ThemePalleteServiceExternal:  "214",
			ThemePalleteServiceCost:      "250",
			ThemePalleteServiceDB:        "39",
			ThemePalleteService3rd:       "178",
		},
		ServicePalette: []string{"68", "173", "71", "176", "74", "179", "109", "175", "75", "181"},
	}

	light := ThemePalette{
		Colors: map[string]string{},
		Pallete: map[string]string{
			ThemePalleteTextFocussed:     "16",
			ThemePalleteTextUnfocussed:   "241",
			ThemePalleteBorder:           "248",
			ThemePalleteBorderActive:     "24",
			ThemePalleteSurfaceA:         "255",
			ThemePalleteSurfaceB:         "254",
			ThemePalleteSurfaceVisual:    "153",
			ThemePalleteSurfaceCursor:    "189",
			ThemePalleteSurfaceCursorVis: "111",
			ThemePalleteSuccess:          "28",
			ThemePalleteInfo:             "25",
			ThemePalleteWarning:          "166",
			ThemePalleteError:            "124",
			ThemePalleteServicePrimary:   "24",
			ThemePalleteServiceSidecar:   "244",
			ThemePalleteServiceExternal:  "166",
			ThemePalleteServiceCost:      "240",
			ThemePalleteServiceDB:        "25",
			ThemePalleteService3rd:       "130",
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

	themes := c.ThemeCatalog()

	selected, found := lookupTheme(themes, name)
	if !found {
		name = "dark"
		selected, _ = lookupTheme(themes, name)
	}
	base, foundBase := lookupTheme(themes, "default")
	if !foundBase {
		base, _ = lookupTheme(DefaultThemes(), "default")
	}
	resolved := mergeThemePalette(base, selected)
	resolved = normalizeThemePalette(resolved)

	return ResolvedTheme{
		Name:     name,
		Palette:  resolved,
		Selected: normalizeThemePalette(selected),
		Default:  normalizeThemePalette(base),
		FromEnv:  fromEnv,
	}
}

func (t ResolvedTheme) ColorFor(key string, hardFallback string) string {
	if color := strings.TrimSpace(t.Selected.Colors[key]); color != "" {
		return color
	}
	paletteKey := colorPalleteFallback[key]
	if paletteKey != "" {
		if color := strings.TrimSpace(t.Selected.Pallete[paletteKey]); color != "" {
			return color
		}
	}
	if color := strings.TrimSpace(t.Default.Colors[key]); color != "" {
		return color
	}
	if paletteKey != "" {
		if color := strings.TrimSpace(t.Default.Pallete[paletteKey]); color != "" {
			return color
		}
	}
	return strings.TrimSpace(hardFallback)
}

func (c Config) ThemeExists(name string) bool {
	_, ok := lookupTheme(c.ThemeCatalog(), name)
	return ok
}

func (c Config) ThemeCatalog() ThemeMap {
	themes := cloneThemeMap(DefaultThemes())
	for key, value := range c.Themes {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if builtInKey := canonicalBuiltInThemeName(trimmed); builtInKey != "" {
			builtIn := themes[builtInKey]
			themes[builtInKey] = mergeThemePalette(builtIn, value)
			continue
		}
		themes[key] = normalizeThemePalette(cloneThemePalette(value))
	}
	return themes
}

func canonicalBuiltInThemeName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "default":
		return "default"
	case "dark":
		return "dark"
	case "light":
		return "light"
	default:
		return ""
	}
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
	palette.Pallete = cloneColorMap(palette.Pallete)
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

func CompressThemePalette(palette ThemePalette) ThemePalette {
	compressed := normalizeThemePalette(cloneThemePalette(palette))
	if len(compressed.Colors) == 0 {
		return compressed
	}
	for key, value := range compressed.Colors {
		paletteKey := colorPalleteFallback[key]
		if paletteKey == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(compressed.Pallete[paletteKey])) {
			delete(compressed.Colors, key)
		}
	}
	return compressed
}

func FlattenThemePalette(resolved ResolvedTheme, palette ThemePalette) ThemePalette {
	flattened := normalizeThemePalette(cloneThemePalette(palette))
	if flattened.Colors == nil {
		flattened.Colors = map[string]string{}
	}
	for _, key := range themeColorKeys {
		if color := strings.TrimSpace(resolved.ColorFor(key, "")); color != "" {
			flattened.Colors[key] = color
		}
	}
	return flattened
}

func mergeThemePalette(base ThemePalette, overlay ThemePalette) ThemePalette {
	merged := normalizeThemePalette(base)
	if merged.Colors == nil {
		merged.Colors = map[string]string{}
	}
	if merged.Pallete == nil {
		merged.Pallete = map[string]string{}
	}
	for key, value := range overlay.Colors {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			merged.Colors[key] = trimmed
		}
	}
	for key, value := range overlay.Pallete {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			merged.Pallete[key] = trimmed
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
	out := ThemePalette{Colors: cloneColorMap(in.Colors), Pallete: cloneColorMap(in.Pallete)}
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

package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/almahoozi/trace/internal/config"
)

var (
	titleStyle                = lipgloss.NewStyle()
	mutedStyle                = lipgloss.NewStyle()
	tableRowBandStyleA        = lipgloss.NewStyle()
	tableRowBandStyleB        = lipgloss.NewStyle()
	tableRowVisualStyle       = lipgloss.NewStyle()
	tableRowCursorStyle       = lipgloss.NewStyle()
	tableRowCursorVisualStyle = lipgloss.NewStyle()

	summaryBrightStyle  = lipgloss.NewStyle()
	summaryGrayStyle    = lipgloss.NewStyle()
	summarySuccessStyle = lipgloss.NewStyle()
	summaryInfoStyle    = lipgloss.NewStyle()
	summaryWarnStyle    = lipgloss.NewStyle()
	summaryErrorStyle   = lipgloss.NewStyle()
)

func init() {
	applyThemeStyles(config.DefaultConfig().ResolveTheme())
}

func applyThemeStyles(theme config.ResolvedTheme) {
	color := theme.ColorFor

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color(config.ThemeColorTitle, "12")))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorMuted, "241")))

	tableRowBandStyleA = lipgloss.NewStyle().Background(lipgloss.Color(color(config.ThemeColorTableBandA, "234")))
	tableRowBandStyleB = lipgloss.NewStyle().Background(lipgloss.Color(color(config.ThemeColorTableBandB, "235")))
	tableRowVisualStyle = lipgloss.NewStyle().Background(lipgloss.Color(color(config.ThemeColorTableVisual, "236")))
	tableRowCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color(color(config.ThemeColorTableCursor, "238")))
	tableRowCursorVisualStyle = lipgloss.NewStyle().Background(lipgloss.Color(color(config.ThemeColorTableCursorVisual, "61")))

	summaryBrightStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummaryBright, "15")))
	summaryGrayStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummaryGray, "245")))
	summarySuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummarySuccess, "2")))
	summaryInfoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummaryInfo, "4")))
	summaryWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummaryWarn, "3")))
	summaryErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color(config.ThemeColorSummaryError, "1")))
}

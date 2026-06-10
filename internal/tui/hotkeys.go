package tui

import "strings"

func isConfigHotkey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "f2", "alt+,", "ctrl+,", "cmd+,", "meta+,":
		return true
	default:
		return false
	}
}

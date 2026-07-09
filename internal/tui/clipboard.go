package tui

import "github.com/atotto/clipboard"

var clipboardWriteAll = clipboard.WriteAll

func writeClipboardText(value string) error {
	return clipboardWriteAll(value)
}

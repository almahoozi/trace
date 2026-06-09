package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func OpenInEditor(path string) error {
	if editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR")); editor != "" {
		parts := strings.Fields(editor)
		cmd := exec.Command(parts[0], append(parts[1:], path)...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-t", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	case "windows":
		cmd = exec.Command("notepad", path)
	default:
		return fmt.Errorf("unsupported platform %q", runtime.GOOS)
	}
	return cmd.Start()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

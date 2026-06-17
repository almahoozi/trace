package buildinfo

import (
	"runtime/debug"
	"strings"
)

type Info struct {
	Version string
}

func Current() Info {
	return Info{Version: detectVersion()}
}

func detectVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}

	mainVersion := strings.TrimSpace(info.Main.Version)
	if mainVersion != "" && mainVersion != "(devel)" {
		return mainVersion
	}

	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			modified = strings.EqualFold(strings.TrimSpace(setting.Value), "true")
		}
	}
	if revision == "" {
		return ""
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}

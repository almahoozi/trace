package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	traceURLFormat     = "https://telemetry.betterstack.com/team/t000000/tail?q=%s&s=%s&rf=now-60m"
	fallbackDefaultEnv = ""
	emptyIsAll         = true
)

var (
	commit, ref, version string
	envs                 = map[string]string{
		"":        "",
		"dev":     "772925",
		"staging": "773432",
		"prod":    "775784",
	}
	versionFlag bool
	dryFlag     bool
)

func init() {
	flag.BoolVar(&versionFlag, "v", false, "print version information and exit")
	flag.BoolVar(&versionFlag, "version", false, "print version information and exit")
	flag.BoolVar(&dryFlag, "d", false, "print the trace URL without opening it")
	flag.BoolVar(&dryFlag, "dry", false, "print the trace URL without opening it")
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-v|--version] [-d|--dry] [env] <trace-id>\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if versionFlag {
		printBuildInfo()
		return
	}

	args := flag.Args()
	url, err := handleArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if dryFlag {
		fmt.Println(url)
		return
	}

	if err := openURL(url); err != nil {
		fmt.Fprintf(os.Stderr, "failed to open browser: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Opening", url)
}

func handleArgs(args []string) (string, error) {
	switch len(args) {
	case 1:
		return buildURL(args[0], defaultEnv())
	case 2:
		env := args[0]
		if !isValidEnv(env) {
			return "", fmt.Errorf("invalid environment %q", env)
		}
		return buildURL(args[1], env)
	default:
		return "", fmt.Errorf("usage: %s [-v|--version] [-d|--dry] [env] <trace-id>", os.Args[0])
	}
}

func defaultEnv() string {
	env, ok := os.LookupEnv("TRACELINK_DEFAULT_ENV")
	if !ok {
		return fallbackDefaultEnv
	}
	if isValidEnv(env) {
		return env
	}
	panic(fmt.Sprintf("invalid TRACELINK_DEFAULT_ENV: %q", env))
}

func buildURL(traceID, env string) (string, error) {
	envID, ok := envs[env]
	if !ok {
		return "", fmt.Errorf("invalid environment %q", env)
	}
	if emptyIsAll && envID == "" {
		var envIDs []string
		for _, id := range envs {
			if id == "" {
				continue
			}
			envIDs = append(envIDs, id)
		}
		envID = strings.Join(envIDs, ",")
	}
	return fmt.Sprintf(traceURLFormat, traceID, envID), nil
}

func isValidEnv(env string) bool {
	_, ok := envs[env]
	return ok
}

func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform %q", runtime.GOOS)
	}
	return cmd.Start()
}

func printBuildInfo() {
	if commit == "" && ref == "" && version == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "build: %s %s %s\n", commit, ref, version)
}

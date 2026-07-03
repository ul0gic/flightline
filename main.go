package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/cmd"
)

// Injected via -ldflags at release build time (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// buildInfoFallback fills version metadata from the Go module build info when
// ldflags weren't set — the `go install module@version` path.
func buildInfoFallback() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		version = v
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.time":
			date = s.Value
		}
	}
}

func main() {
	if version == "dev" {
		buildInfoFallback()
	}
	cmd.SetBuildInfo(version, commit, date)
	err := cmd.Execute()
	if err == nil {
		return
	}
	// Redact at the outermost printer so every error path is cred-stripped before stderr (SEC-002).
	if msg := err.Error(); msg != "" {
		fmt.Fprintf(os.Stderr, "flightline: %s\n", asc.Redact(msg))
	}
	os.Exit(cmd.ExitCode(err))
}

package main

import (
	"fmt"
	"os"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/cmd"
)

// Injected via -ldflags at release build time (see .goreleaser.yaml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
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

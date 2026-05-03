package main

import (
	"fmt"
	"os"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Redact at the outermost printer so any error path (auth, fs, net,
		// not just *APIError) gets cred-stripped before reaching stderr.
		// See .project/issues/closed/SEC-002 for the keyID/UUID/AuthKey-path
		// regression that motivated promoting Redact() to public API.
		fmt.Fprintf(os.Stderr, "fline: %s\n", asc.Redact(err.Error()))
		os.Exit(1)
	}
}

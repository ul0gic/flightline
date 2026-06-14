package main

import (
	"fmt"
	"os"

	"github.com/ul0gic/flightline/internal/asc"
	"github.com/ul0gic/flightline/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Redact at the outermost printer so every error path is cred-stripped before stderr (SEC-002).
		fmt.Fprintf(os.Stderr, "flightline: %s\n", asc.Redact(err.Error()))
		os.Exit(1)
	}
}

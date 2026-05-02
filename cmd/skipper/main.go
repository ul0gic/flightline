package main

import (
	"fmt"
	"os"

	"github.com/ul0gic/skipper/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "skipper: %v\n", err)
		os.Exit(1)
	}
}

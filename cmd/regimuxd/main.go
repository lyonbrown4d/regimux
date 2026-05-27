// Package main runs the RegiMux registry proxy daemon.
package main

import (
	"os"
	"log/slog"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		slog.Error("regimuxd failed", "error", err)
		os.Exit(1)
	}
}

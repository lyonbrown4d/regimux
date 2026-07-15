// Package main defines the regimux build and release pipeline.
package main

import (
	"strconv"
	"strings"

	"github.com/goyek/goyek/v3"
	goyekcmd "github.com/goyek/x/cmd"
)

func runCommand(
	a *goyek.A,
	name string,
	args []string,
	options ...goyekcmd.Option,
) {
	a.Helper()
	if !goyekcmd.Exec(a, commandLine(name, args...), options...) {
		a.FailNow()
	}
}

func commandOutput(a *goyek.A, directory, name string, args ...string) string {
	a.Helper()

	var output strings.Builder
	if !goyekcmd.Exec(
		a,
		commandLine(name, args...),
		goyekcmd.Dir(directory),
		goyekcmd.Stdout(&output),
	) {
		a.FailNow()
	}
	return strings.TrimSpace(output.String())
}

func commandLine(name string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteCommandArgument(name))
	for _, argument := range args {
		parts = append(parts, quoteCommandArgument(argument))
	}
	return strings.Join(parts, " ")
}

func quoteCommandArgument(argument string) string {
	if argument != "" && strings.IndexFunc(argument, isUnsafeCommandRune) == -1 {
		return argument
	}
	return strconv.Quote(argument)
}

func isUnsafeCommandRune(value rune) bool {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-"
	return !strings.ContainsRune(safe, value)
}

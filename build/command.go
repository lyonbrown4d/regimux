// Package main defines the RegiMux build and release pipeline.
package main

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/goyek/goyek/v3"
	goyekcmd "github.com/goyek/x/cmd"
)

const redactedCommandValue = "[REDACTED]"

type commandEnvironment struct {
	Name   string
	Value  string
	Secret bool
}

func publicCommandEnvironment(name, value string) commandEnvironment {
	return commandEnvironment{Name: name, Value: value}
}

func secretCommandEnvironment(name, value string) commandEnvironment {
	return commandEnvironment{Name: name, Value: value, Secret: true}
}

func (environment commandEnvironment) logValue() string {
	if environment.Secret {
		return redactedCommandValue
	}
	return environment.Value
}

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

func runCommandWithEnvironment(
	a *goyek.A,
	directory string,
	name string,
	args []string,
	environment ...commandEnvironment,
) {
	a.Helper()
	logCommand(a, directory, name, args, environment)
	if !goyekcmd.Exec(
		a,
		commandLine(name, args...),
		goyekcmd.Dir(directory),
		commandEnvironmentOption(environment),
	) {
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

func commandOutputWithEnvironment(
	a *goyek.A,
	directory string,
	name string,
	args []string,
	environment ...commandEnvironment,
) string {
	a.Helper()
	logCommand(a, directory, name, args, environment)

	var output strings.Builder
	if !goyekcmd.Exec(
		a,
		commandLine(name, args...),
		goyekcmd.Dir(directory),
		commandEnvironmentOption(environment),
		goyekcmd.Stdout(&output),
	) {
		a.FailNow()
	}
	return strings.TrimSpace(output.String())
}

func commandEnvironmentOption(
	environment []commandEnvironment,
) goyekcmd.Option {
	return func(_ *goyek.A, command *exec.Cmd) {
		command.Env = command.Environ()
		for _, variable := range environment {
			command.Env = append(
				command.Env,
				variable.Name+"="+variable.Value,
			)
		}
	}
}

func logCommand(
	a *goyek.A,
	directory string,
	name string,
	args []string,
	environment []commandEnvironment,
) {
	if directory != "" {
		a.Logf("Work dir: %s", directory)
	}
	for _, variable := range environment {
		a.Logf("Env: %s=%s", variable.Name, variable.logValue())
	}
	a.Logf("Exec: %s", commandLine(name, args...))
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

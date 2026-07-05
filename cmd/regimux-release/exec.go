//go:build release

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func runDocker(ctx context.Context, args []string, extraEnv map[string]string, dryRun bool) error {
	if err := writeStdoutf("+ docker %s\n", formatArgs(args)); err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = mergedEnv(extraEnv)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func writeStdoutf(format string, args ...any) error {
	if _, err := fmt.Fprintf(os.Stdout, format, args...); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	return nil
}
func dockerVolume(host, container string) string {
	return host + ":" + container
}

func mergedEnv(extra map[string]string) []string {
	env := os.Environ()
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func formatArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.IndexFunc(arg, needsQuote) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func needsQuote(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\'' || r == '"'
}

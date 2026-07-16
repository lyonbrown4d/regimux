package main

import (
	"strings"
	"testing"
)

func TestCommandEnvironmentLogValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment commandEnvironment
		want        string
	}{
		{
			name:        "public",
			environment: publicCommandEnvironment("IMAGE", "regimux:latest"),
			want:        "regimux:latest",
		},
		{
			name:        "secret",
			environment: secretCommandEnvironment("GITHUB_TOKEN", "top-secret"),
			want:        redactedCommandValue,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := test.environment.logValue(); actual != test.want {
				t.Fatalf("logValue() = %q, want %q", actual, test.want)
			}
		})
	}
}

func TestSecretCommandEnvironmentNeverFormatsValue(t *testing.T) {
	t.Parallel()

	const secret = "opaque-sensitive-value"
	environment := secretCommandEnvironment("GITHUB_TOKEN", secret)
	logLine := environment.Name + "=" + environment.logValue()
	if strings.Contains(logLine, secret) {
		t.Fatalf("secret leaked in log line %q", logLine)
	}
	if !strings.Contains(logLine, redactedCommandValue) {
		t.Fatalf("redaction marker missing from log line %q", logLine)
	}
}

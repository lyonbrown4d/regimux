package object_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/store/object"
)

func TestSFTPStoreRejectsMissingAddress(t *testing.T) {
	_, err := object.NewWithOptions(context.Background(), object.Options{
		Driver: "sftp",
		SFTP: object.SFTPOptions{
			Username: "regimux",
			Password: "secret",
			HostKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeHostKeyForConfigValidationOnly",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "addr") {
		t.Fatalf("expected missing addr error, got %v", err)
	}
}

func TestSFTPStoreRejectsMissingHostKeyVerification(t *testing.T) {
	_, err := object.NewWithOptions(context.Background(), object.Options{
		Driver: "sftp",
		SFTP: object.SFTPOptions{
			Addr:     "127.0.0.1:1",
			Username: "regimux",
			Password: "secret",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "known_hosts_path") {
		t.Fatalf("expected missing host key verification error, got %v", err)
	}
}

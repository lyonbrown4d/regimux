package spool_test

import (
	"os"
	"testing"

	"github.com/lyonbrown4d/regimux/internal/spool"
)

func TestNewTempBodyRemovesFileOnClose(t *testing.T) {
	body, err := spool.NewTempBody("regimux-stream-test-*")
	if err != nil {
		t.Fatal(err)
	}
	name := body.Name()
	if _, err := body.WriteString("streamed payload"); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRun_ReturnsErrorWhenConfigInvalid(t *testing.T) {
	var stderr bytes.Buffer
	err := run(context.Background(), func(string) string { return "" }, &stderr)
	if err == nil {
		t.Fatal("expected an error when required env vars are missing, got nil")
	}
}

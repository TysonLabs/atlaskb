package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestMainVersionCommand(t *testing.T) {
	oldArgs := os.Args
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	os.Args = []string{"atlaskb", "version"}

	main()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("expected non-empty version output")
	}
}

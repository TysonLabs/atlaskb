package web

import "testing"

func TestDistFS(t *testing.T) {
	// Ensure DistFS is safe to call in tests regardless of embedded dist contents.
	_ = DistFS()
}

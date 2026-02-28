package pipeline

import "testing"

func TestQualifiedNameOwner(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"storage::MemoryStorage.Save", "storage::MemoryStorage"},
		{"storage::FileStorage.Save", "storage::FileStorage"},
		{"api::Service.Publish", "api::Service"},
		{"api::Handler.Publish", "api::Handler"},
		{"bus::Bus.Publish", "bus::Bus"},
		{"storage::NewMemoryStorage", "storage"},   // top-level, no dot
		{"models::Task", "models"},                  // top-level, no dot
		{"middleware::LoggingMiddleware.Wrap", "middleware::LoggingMiddleware"},
		{"middleware::RetryMiddleware.Wrap", "middleware::RetryMiddleware"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := qualifiedNameOwner(tt.input)
			if got != tt.want {
				t.Errorf("qualifiedNameOwner(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQualifiedNamePackage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"store::TaskStore.Create", "store"},
		{"models::Task", "models"},
		{"main::main", "main"},
		{"no-separator", "no-separator"},
		{"", ""},
		{"pkg::nested::deep", "pkg"},
		{"api::Service.Publish", "api"},
		{"bus::Bus.Publish", "bus"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := qualifiedNamePackage(tt.input)
			if got != tt.want {
				t.Errorf("qualifiedNamePackage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

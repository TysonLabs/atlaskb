package pipeline

import "testing"

func TestClassifyFile(t *testing.T) {
	tests := []struct {
		path     string
		size     int64
		wantClass FileClass
		wantLang  string
	}{
		{"cmd/main.go", 1000, ClassSource, "go"},
		{"internal/handler_test.go", 500, ClassTest, "go"},
		{"vendor/lib/foo.go", 200, ClassVendored, ""},
		{"node_modules/foo/index.js", 100, ClassVendored, ""},
		{"README.md", 300, ClassDoc, "markdown"},
		{"go.mod", 100, ClassBuild, ""},
		{"Dockerfile", 200, ClassConfig, ""},
		{"logo.png", 50000, ClassIgnored, ""},
		{"api.pb.go", 10000, ClassGenerated, ""},
		{"src/app.tsx", 2000, ClassSource, "typescript"},
		{"tests/test_handler.py", 800, ClassTest, "python"},
		{"src/utils.test.js", 600, ClassTest, "javascript"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			fi := ClassifyFile(tt.path, tt.size)
			if fi.Class != tt.wantClass {
				t.Errorf("ClassifyFile(%q).Class = %q, want %q", tt.path, fi.Class, tt.wantClass)
			}
			if fi.Language != tt.wantLang {
				t.Errorf("ClassifyFile(%q).Language = %q, want %q", tt.path, fi.Language, tt.wantLang)
			}
		})
	}
}

func TestShouldAnalyze(t *testing.T) {
	tests := []struct {
		class FileClass
		want  bool
	}{
		{ClassSource, true},
		{ClassTest, true},
		{ClassConfig, true},
		{ClassDoc, true},
		{ClassBuild, true},
		{ClassVendored, false},
		{ClassGenerated, false},
		{ClassIgnored, false},
	}

	for _, tt := range tests {
		fi := FileInfo{Class: tt.class}
		if got := ShouldAnalyze(fi); got != tt.want {
			t.Errorf("ShouldAnalyze(%q) = %v, want %v", tt.class, got, tt.want)
		}
	}
}

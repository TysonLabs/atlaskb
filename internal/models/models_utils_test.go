package models

import "testing"

func TestPtr(t *testing.T) {
	v := 42
	p := Ptr(v)
	if p == nil {
		t.Fatal("Ptr() returned nil")
	}
	if *p != 42 {
		t.Fatalf("*Ptr(42) = %d, want 42", *p)
	}
}

func TestDefaultTraversalOptions(t *testing.T) {
	got := DefaultTraversalOptions()
	if got.MaxHops != 3 {
		t.Fatalf("MaxHops = %d, want 3", got.MaxHops)
	}
	if got.MaxEntities != 200 {
		t.Fatalf("MaxEntities = %d, want 200", got.MaxEntities)
	}
	if got.IncludeFacts {
		t.Fatal("IncludeFacts = true, want false")
	}
	if got.FactsPerEntity != 10 {
		t.Fatalf("FactsPerEntity = %d, want 10", got.FactsPerEntity)
	}
}

func TestStrengthToConfidenceDelta(t *testing.T) {
	if got := StrengthToConfidenceDelta(StrengthStrong); got != 0.05 {
		t.Fatalf("strong delta = %v, want 0.05", got)
	}
	if got := StrengthToConfidenceDelta(StrengthWeak); got != -0.05 {
		t.Fatalf("weak delta = %v, want -0.05", got)
	}
	if got := StrengthToConfidenceDelta(StrengthModerate); got != 0 {
		t.Fatalf("moderate delta = %v, want 0", got)
	}
}

func TestClampConfidence(t *testing.T) {
	tests := []struct {
		name string
		in   float32
		want float32
	}{
		{name: "negative", in: -0.2, want: 0},
		{name: "in-range", in: 0.75, want: 0.75},
		{name: "high", in: 1.8, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampConfidence(tc.in); got != tc.want {
				t.Fatalf("ClampConfidence(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

package models

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestComputeRepoStaleness_NeverIndexed(t *testing.T) {
	repo := Repo{
		ID:   uuid.New(),
		Name: "demo",
	}
	s := ComputeRepoStaleness(context.Background(), repo)
	if !s.Stale {
		t.Fatalf("expected stale=true for never indexed repo")
	}
	if len(s.Reasons) == 0 || s.Reasons[0] != "never_indexed" {
		t.Fatalf("expected reason never_indexed, got %v", s.Reasons)
	}
}

func TestComputeRepoStaleness_Age(t *testing.T) {
	old := time.Now().Add(-(DefaultStaleAfter + time.Hour))
	repo := Repo{
		ID:           uuid.New(),
		Name:         "demo",
		LastIndexedAt: &old,
	}
	s := ComputeRepoStaleness(context.Background(), repo)
	if !s.Stale {
		t.Fatalf("expected stale=true for old index")
	}
	found := false
	for _, reason := range s.Reasons {
		if reason == "index_age" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected index_age reason, got %v", s.Reasons)
	}
}

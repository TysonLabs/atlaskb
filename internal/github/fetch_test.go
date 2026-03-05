package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgeorge06/atlaskb/internal/config"
)

func TestFetchMergedPRsSuccess(t *testing.T) {
	mergedAt := "2026-03-05T10:00:00Z"
	resetAt := "2026-03-05T10:05:00Z"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("authorization header = %q, want Bearer token", got)
		}

		// Verify we received a GraphQL payload.
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := payload["query"]; !ok {
			t.Fatalf("graphql payload missing query: %+v", payload)
		}

		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   "",
						},
						"nodes": []map[string]any{
							{
								"number":   42,
								"title":    "Refactor indexing",
								"url":      "https://example/pr/42",
								"body":     "PR body",
								"mergedAt": mergedAt,
								"author":   map[string]any{"login": "octocat"},
								"labels": map[string]any{
									"nodes": []map[string]any{{"name": "enhancement"}},
								},
								"reviews": map[string]any{
									"nodes": []map[string]any{
										{
											"author": map[string]any{"login": "reviewer1"},
											"body":   "Looks good",
											"state":  "APPROVED",
										},
										{
											"author": map[string]any{"login": "reviewer2"},
											"body":   "",
											"state":  "COMMENTED",
										},
									},
								},
								"closingIssuesReferences": map[string]any{
									"nodes": []map[string]any{
										{
											"number": 7,
											"title":  "Track follow-up",
											"body":   "Issue body",
											"labels": map[string]any{
												"nodes": []map[string]any{{"name": "bug"}},
											},
										},
									},
								},
							},
						},
					},
				},
				"rateLimit": map[string]any{
					"remaining": 100,
					"resetAt":   resetAt,
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(config.GitHubConfig{
		Token:  "token",
		APIURL: srv.URL,
	})

	prs, err := client.FetchMergedPRs(context.Background(), "owner", "repo", 5)
	if err != nil {
		t.Fatalf("FetchMergedPRs() error = %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("len(prs) = %d, want 1", len(prs))
	}

	pr := prs[0]
	if pr.Number != 42 || pr.Title != "Refactor indexing" || pr.Author != "octocat" {
		t.Fatalf("unexpected PR fields: %+v", pr)
	}
	if pr.MergedAt.IsZero() {
		t.Fatal("mergedAt should be parsed")
	}
	if len(pr.Labels) != 1 || pr.Labels[0] != "enhancement" {
		t.Fatalf("labels = %+v, want [enhancement]", pr.Labels)
	}
	// Empty review body should be filtered.
	if len(pr.ReviewComments) != 1 || pr.ReviewComments[0].Body != "Looks good" {
		t.Fatalf("review comments = %+v", pr.ReviewComments)
	}
	if len(pr.LinkedIssues) != 1 || pr.LinkedIssues[0].Number != 7 {
		t.Fatalf("linked issues = %+v", pr.LinkedIssues)
	}
}

func TestFetchMergedPRsQueryError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(config.GitHubConfig{
		Token:  "token",
		APIURL: srv.URL,
	})

	_, err := client.FetchMergedPRs(context.Background(), "owner", "repo", 5)
	if err == nil || !strings.Contains(err.Error(), "GitHub GraphQL query") {
		t.Fatalf("FetchMergedPRs() error = %v, want GraphQL query error", err)
	}
}

func TestFetchMergedPRsContextCancel(t *testing.T) {
	// Trigger the low-rate-limit sleep branch, then cancel.
	now := time.Now().UTC()
	resetAt := now.Add(2 * time.Second).Format(time.RFC3339)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequests": map[string]any{
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   "",
						},
						"nodes": []map[string]any{},
					},
				},
				"rateLimit": map[string]any{
					"remaining": 1,
					"resetAt":   resetAt,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(config.GitHubConfig{
		Token:  "token",
		APIURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.FetchMergedPRs(ctx, "owner", "repo", 5)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("FetchMergedPRs() error = %v, want context canceled", err)
	}
}

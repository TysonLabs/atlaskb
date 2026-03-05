package github

import (
	"testing"

	"github.com/tgeorge06/atlaskb/internal/config"
)

func TestNewClient(t *testing.T) {
	c := NewClient(config.GitHubConfig{
		Token:  "token",
		APIURL: "https://api.github.com/graphql",
	})
	if c == nil || c.gql == nil {
		t.Fatal("NewClient() returned nil client")
	}
	if c.cfg.Token != "token" {
		t.Fatalf("client cfg token = %q, want token", c.cfg.Token)
	}

	enterprise := NewClient(config.GitHubConfig{
		Token:  "enterprise-token",
		APIURL: "https://ghe.example.com/api/graphql",
	})
	if enterprise == nil || enterprise.gql == nil {
		t.Fatal("NewClient() enterprise returned nil client")
	}
	if enterprise.cfg.APIURL != "https://ghe.example.com/api/graphql" {
		t.Fatalf("enterprise API URL = %q", enterprise.cfg.APIURL)
	}
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		owner  string
		repo   string
		ok     bool
	}{
		{name: "https", remote: "https://github.com/tgeorge06/atlaskb.git", owner: "tgeorge06", repo: "atlaskb", ok: true},
		{name: "ssh short", remote: "git@github.com:tgeorge06/atlaskb.git", owner: "tgeorge06", repo: "atlaskb", ok: true},
		{name: "ssh long", remote: "ssh://git@github.com/tgeorge06/atlaskb", owner: "tgeorge06", repo: "atlaskb", ok: true},
		{name: "empty", remote: "", ok: false},
		{name: "invalid", remote: "not-a-url", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, ok := ParseRemoteURL(tc.remote)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if owner != tc.owner || repo != tc.repo {
				t.Fatalf("owner/repo = %q/%q, want %q/%q", owner, repo, tc.owner, tc.repo)
			}
		})
	}
}

func TestIsGitHubRemote(t *testing.T) {
	tests := []struct {
		name           string
		remote         string
		enterpriseHost string
		want           bool
	}{
		{name: "github dot com", remote: "https://github.com/org/repo.git", want: true},
		{name: "enterprise host match", remote: "https://git.acme.internal/org/repo.git", enterpriseHost: "git.acme.internal", want: true},
		{name: "enterprise host case-insensitive", remote: "SSH://git@GIT.ACME.INTERNAL/org/repo", enterpriseHost: "git.acme.internal", want: true},
		{name: "no match", remote: "https://gitlab.com/org/repo", enterpriseHost: "git.acme.internal", want: false},
		{name: "empty", remote: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsGitHubRemote(tc.remote, tc.enterpriseHost); got != tc.want {
				t.Fatalf("IsGitHubRemote(%q, %q) = %v, want %v", tc.remote, tc.enterpriseHost, got, tc.want)
			}
		})
	}
}

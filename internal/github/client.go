package github

import (
	"context"
	"regexp"
	"strings"

	"github.com/shurcooL/githubv4"
	"github.com/tgeorge06/atlaskb/internal/config"
	"golang.org/x/oauth2"
)

type Client struct {
	gql *githubv4.Client
	cfg config.GitHubConfig
}

func NewClient(cfg config.GitHubConfig) *Client {
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})
	httpClient := oauth2.NewClient(context.Background(), src)

	var gql *githubv4.Client
	if cfg.APIURL != "" && cfg.APIURL != "https://api.github.com/graphql" {
		gql = githubv4.NewEnterpriseClient(cfg.APIURL, httpClient)
	} else {
		gql = githubv4.NewClient(httpClient)
	}

	return &Client{gql: gql, cfg: cfg}
}

var (
	httpsPattern = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/([^/.]+)(?:\.git)?$`)
	sshPattern   = regexp.MustCompile(`^(?:ssh://)?git@([^:/]+)[:/]([^/]+)/([^/.]+?)(?:\.git)?$`)
)

// ParseRemoteURL extracts owner and repo from a GitHub remote URL.
// Supports both HTTPS and SSH formats.
func ParseRemoteURL(remoteURL string) (owner, repo string, ok bool) {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return "", "", false
	}

	if m := httpsPattern.FindStringSubmatch(remoteURL); m != nil {
		return m[2], m[3], true
	}
	if m := sshPattern.FindStringSubmatch(remoteURL); m != nil {
		return m[2], m[3], true
	}
	return "", "", false
}

// IsGitHubRemote checks if a remote URL points to GitHub (or a GHE instance).
func IsGitHubRemote(remoteURL, enterpriseHost string) bool {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return false
	}

	lower := strings.ToLower(remoteURL)
	if strings.Contains(lower, "github.com") {
		return true
	}
	if enterpriseHost != "" && strings.Contains(lower, strings.ToLower(enterpriseHost)) {
		return true
	}
	return false
}

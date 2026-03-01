package github

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shurcooL/githubv4"
)

type PR struct {
	Number         int
	Title          string
	URL            string
	Author         string
	MergedAt       time.Time
	Body           string
	Labels         []string
	ReviewComments []ReviewComment
	LinkedIssues   []Issue
}

type ReviewComment struct {
	Author string
	Body   string
	State  string // APPROVED, CHANGES_REQUESTED, COMMENTED
}

type Issue struct {
	Number int
	Title  string
	Body   string
	Labels []string
}

// GraphQL query struct for fetching merged PRs with reviews and linked issues.
type prQuery struct {
	Repository struct {
		PullRequests struct {
			PageInfo struct {
				HasNextPage bool
				EndCursor   githubv4.String
			}
			Nodes []struct {
				Number    int
				Title     string
				URL       string
				Body      string
				MergedAt  *time.Time
				Author    struct {
					Login string
				}
				Labels struct {
					Nodes []struct {
						Name string
					}
				} `graphql:"labels(first: 10)"`
				Reviews struct {
					Nodes []struct {
						Author struct {
							Login string
						}
						Body  string
						State string
					}
				} `graphql:"reviews(first: 20)"`
				ClosingIssuesReferences struct {
					Nodes []struct {
						Number int
						Title  string
						Body   string
						Labels struct {
							Nodes []struct {
								Name string
							}
						} `graphql:"labels(first: 10)"`
					}
				} `graphql:"closingIssuesReferences(first: 5)"`
			}
		} `graphql:"pullRequests(states: MERGED, first: $pageSize, after: $cursor, orderBy: {field: UPDATED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $repo)"`
	RateLimit struct {
		Remaining int
		ResetAt   time.Time
	}
}

// FetchMergedPRs fetches up to maxPRs merged pull requests from the given repository,
// including review comments and linked issues.
func (c *Client) FetchMergedPRs(ctx context.Context, owner, repo string, maxPRs int) ([]PR, error) {
	var allPRs []PR
	var cursor *githubv4.String
	pageSize := 25

	for len(allPRs) < maxPRs {
		remaining := maxPRs - len(allPRs)
		if remaining < pageSize {
			pageSize = remaining
		}

		variables := map[string]interface{}{
			"owner":    githubv4.String(owner),
			"repo":     githubv4.String(repo),
			"pageSize": githubv4.Int(pageSize),
			"cursor":   cursor,
		}

		var q prQuery
		if err := c.gql.Query(ctx, &q, variables); err != nil {
			return allPRs, fmt.Errorf("GitHub GraphQL query: %w", err)
		}

		// Rate limit protection
		if q.RateLimit.Remaining < 50 {
			sleepDuration := time.Until(q.RateLimit.ResetAt) + time.Second
			if sleepDuration > 0 && sleepDuration < 15*time.Minute {
				log.Printf("[github] rate limit low (%d remaining), sleeping %s", q.RateLimit.Remaining, sleepDuration.Round(time.Second))
				select {
				case <-ctx.Done():
					return allPRs, ctx.Err()
				case <-time.After(sleepDuration):
				}
			}
		}

		for _, node := range q.Repository.PullRequests.Nodes {
			if node.MergedAt == nil {
				continue
			}

			pr := PR{
				Number:   node.Number,
				Title:    node.Title,
				URL:      node.URL,
				Author:   node.Author.Login,
				MergedAt: *node.MergedAt,
				Body:     node.Body,
			}

			for _, l := range node.Labels.Nodes {
				pr.Labels = append(pr.Labels, l.Name)
			}

			for _, r := range node.Reviews.Nodes {
				if r.Body == "" {
					continue
				}
				pr.ReviewComments = append(pr.ReviewComments, ReviewComment{
					Author: r.Author.Login,
					Body:   r.Body,
					State:  r.State,
				})
			}

			for _, issue := range node.ClosingIssuesReferences.Nodes {
				i := Issue{
					Number: issue.Number,
					Title:  issue.Title,
					Body:   issue.Body,
				}
				for _, l := range issue.Labels.Nodes {
					i.Labels = append(i.Labels, l.Name)
				}
				pr.LinkedIssues = append(pr.LinkedIssues, i)
			}

			allPRs = append(allPRs, pr)
		}

		if !q.Repository.PullRequests.PageInfo.HasNextPage {
			break
		}
		cursor = &q.Repository.PullRequests.PageInfo.EndCursor
	}

	return allPRs, nil
}

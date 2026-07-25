package mcp

import (
	"context"

	"github.com/srinikhiltumu/go-searxng-mcp/internal/fetch"
	"github.com/srinikhiltumu/go-searxng-mcp/internal/searxng"
)

// searxngSearcher adapts *searxng.Client to the Searcher interface.
type searxngSearcher struct {
	client *searxng.Client
}

func (a *searxngSearcher) Search(ctx context.Context, query string, limit int, category string, language string) ([]searchResult, error) {
	results, err := a.client.Search(ctx, query, limit, category, language)
	if err != nil {
		return nil, err
	}
	out := make([]searchResult, len(results))
	for i, r := range results {
		out[i] = searchResult{
			Rank:    r.Rank,
			Title:   r.Title,
			URL:     r.URL,
			Engine:  r.Engine,
			Snippet: r.Snippet,
		}
	}
	return out, nil
}

func (a *searxngSearcher) Health(ctx context.Context) *healthStatus {
	h := a.client.Health(ctx)
	return &healthStatus{
		Status:            h.Status,
		SearxngURL:        h.SearxngURL,
		SearxngLatencyMs:  h.SearxngLatencyMs,
		EnginesConfigured: h.EnginesConfigured,
		Error:             h.Error,
	}
}

// fetchPageReader adapts *fetch.Fetcher to the PageReader interface.
type fetchPageReader struct {
	fetcher *fetch.Fetcher
}

func (a *fetchPageReader) ReadPage(ctx context.Context, targetURL string, maxChars int) (*readResult, error) {
	result, err := a.fetcher.ReadPage(ctx, targetURL, maxChars)
	if err != nil {
		return nil, err
	}
	return &readResult{
		URL:       result.URL,
		Title:     result.Title,
		Content:   result.Content,
		CharCount: result.CharCount,
		Truncated: result.Truncated,
	}, nil
}

// NewSearxngSearcher creates a Searcher backed by a real searxng.Client.
func NewSearxngSearcher(client *searxng.Client) Searcher {
	return &searxngSearcher{client: client}
}

// NewSearxngHealthChecker creates a HealthChecker backed by a real searxng.Client.
func NewSearxngHealthChecker(client *searxng.Client) HealthChecker {
	return &searxngSearcher{client: client}
}

// NewFetchPageReader creates a PageReader backed by a real fetch.Fetcher.
func NewFetchPageReader(fetcher *fetch.Fetcher) PageReader {
	return &fetchPageReader{fetcher: fetcher}
}

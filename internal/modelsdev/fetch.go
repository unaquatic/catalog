package modelsdev

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultSource is the origin serving the catalog document.
const DefaultSource = "https://models.dev"

// documentPath is the catalog document served by a source.
const documentPath = "/api.json"

// maxDocument bounds the catalog document size accepted from a source. The
// live catalog is a few megabytes; the bound exists to reject a runaway or
// hostile response, not to constrain normal growth.
const maxDocument = 256 << 20

// DefaultTimeout bounds a single catalog fetch.
const DefaultTimeout = 30 * time.Second

// Response is the result of a catalog fetch.
type Response struct {
	// Data is the catalog document.
	Data []byte
}

// Fetcher loads catalog documents from a models.dev compatible source.
type Fetcher struct {
	client *http.Client
	source string
}

// NewFetcher returns a Fetcher reading from source. An empty source selects
// DefaultSource and a nil client selects one bounded by DefaultTimeout.
func NewFetcher(source string, client *http.Client) *Fetcher {
	if source == "" {
		source = DefaultSource
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Fetcher{client: client, source: source}
}

// Fetch returns the catalog document.
func (f *Fetcher) Fetch(ctx context.Context) (*Response, error) {
	return f.FetchPath(ctx, documentPath)
}

// FetchPath returns the document at path of the source, such as
// BundlePath, as Fetch returns the catalog document.
func (f *Fetcher) FetchPath(ctx context.Context, path string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.source+path, nil)
	if err != nil {
		return nil, fmt.Errorf("catalog: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog: fetch: unexpected status %s", resp.Status)
	}

	// Read one byte past the bound so an oversized document is reported rather
	// than silently truncated into a decode failure.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDocument+1))
	if err != nil {
		return nil, fmt.Errorf("catalog: read body: %w", err)
	}
	if len(data) > maxDocument {
		return nil, fmt.Errorf("catalog: document exceeds %d bytes", maxDocument)
	}
	return &Response{Data: data}, nil
}

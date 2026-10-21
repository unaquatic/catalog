package gateway

import (
	"cmp"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/unaquatic/catalog/modelsdev"
)

// DefaultSource is the catalog server flow reads its catalog from, as
// cmd/catalog serves it, until one is hosted.
const DefaultSource = "http://127.0.0.1:4100"

// DocumentPath is where a catalog server serves the whole catalog.
const DocumentPath = "/catalog.json"

// StoreFile is the file in its directory where a Store caches the catalog.
const StoreFile = "catalog.json"

// DefaultTTL is how old the cached catalog may get before a refresh is due.
const DefaultTTL = 24 * time.Hour

// RetryDelay is how long after a refresh starts that another can, so a
// failed refresh waits that long to try again.
const RetryDelay = 5 * time.Minute

// ErrSource reports that the catalog server could not be reached, or
// answered with something other than a catalog of format Version.
var ErrSource = errors.New("catalog server failed")

// StoreOptions configures a Store.
type StoreOptions struct {
	// Dir is the directory of the cache file, StoreFile.
	Dir string
	// Source is the catalog server. Empty selects DefaultSource.
	Source string
	// TTL is how old the catalog may get before a refresh is due. Zero
	// selects DefaultTTL.
	TTL time.Duration
	// Client performs the requests. Nil selects one bounded by
	// modelsdev.DefaultTimeout.
	Client *http.Client
	// Snapshot returns the catalog Get returns while the cache holds none,
	// such as one a program compiles in for a cold start with no cache and
	// no server, as Keep cuts it. Nil returns an empty catalog.
	Snapshot func() *Catalog
}

// Store holds the catalog that a catalog server serves, as flow reads it:
// downloaded from the server's DocumentPath and cached in a file, which
// holds its state, so that every process sees the latest download. A read
// never waits for the network: it takes the cached catalog, however old, or
// else its StoreOptions.Snapshot. The store starts nothing on its own: its owner asks
// Due, and calls Refresh, which waits for the download, when it reports
// true. A Store is safe for concurrent use; the catalogs it returns are
// shared and must not be changed.
type Store struct {
	cache    *modelsdev.Cache
	fetcher  *modelsdev.Fetcher
	ttl      time.Duration
	snapshot func() *Catalog

	mu sync.Mutex
	// read is the catalog last decoded from the cache file, and stamp the
	// file as it was then, so that Get decodes it again only once it
	// changes.
	read  *Catalog
	stamp stamp
}

// stamp tells one version of a file from another.
type stamp struct {
	mod  time.Time
	size int64
}

// NewStore returns a Store configured by opts.
func NewStore(opts StoreOptions) *Store {
	s := &Store{
		cache:    modelsdev.NewCacheFile(opts.Dir, StoreFile),
		fetcher:  modelsdev.NewFetcher(cmp.Or(opts.Source, DefaultSource), opts.Client),
		ttl:      cmp.Or(opts.TTL, DefaultTTL),
		snapshot: opts.Snapshot,
	}
	if s.snapshot == nil {
		s.snapshot = emptyCatalog
	}
	return s
}

// emptyCatalog returns a catalog of no models and no providers.
func emptyCatalog() *Catalog {
	return &Catalog{Version: Version, Models: map[ModelID]Model{}, Providers: map[ProviderID]Provider{}}
}

// Path returns the cache file.
func (s *Store) Path() string { return s.cache.Path() }

// Get returns the catalog: the cached one, however old, or else the
// snapshot when the cache holds none or cannot be read.
func (s *Store) Get() *Catalog {
	info, err := os.Stat(s.cache.Path())
	if err != nil {
		return s.snapshot()
	}
	st := stamp{mod: info.ModTime(), size: info.Size()}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.read != nil && s.stamp == st {
		return s.read
	}
	entry := s.cached()
	if entry == nil || len(entry.Data) == 0 {
		return s.snapshot()
	}
	c, err := Decode(entry.Data)
	if err != nil {
		slog.Warn("cached catalog unreadable", "path", s.cache.Path(), "err", err)
		return s.snapshot()
	}
	s.read, s.stamp = c, st
	return c
}

// Due reports whether a refresh is due: the cache holds no download, or one
// older than the TTL, and no refresh started within RetryDelay. When it is,
// Due records in the cache that one has started, under the cache's lock, so
// that no other caller, in this process or another, is told one is due
// within RetryDelay; the caller is then to call Refresh.
func (s *Store) Due() (bool, error) {
	if entry, err := s.cache.Load(); err == nil && !s.DueFor(entry) {
		return false, nil
	}
	claimed := false
	err := s.cache.Update(func(entry *modelsdev.Entry) (*modelsdev.Entry, error) {
		if !s.DueFor(entry) {
			return nil, nil
		}
		claimed = true
		if entry == nil {
			entry = &modelsdev.Entry{}
		}
		entry.Checked = time.Now()
		return entry, nil
	})
	return claimed, err
}

// DueFor reports whether a refresh is due for the cached entry, nil when
// there is none, as Due reads it.
func (s *Store) DueFor(entry *modelsdev.Entry) bool {
	if entry == nil {
		return true
	}
	if len(entry.Data) > 0 && !entry.Stale(s.ttl) {
		return false
	}
	return time.Since(entry.Checked) >= RetryDelay
}

// Refresh downloads the catalog from the server now, whatever its age,
// within modelsdev.DefaultTimeout or until ctx ends, stores it in the cache,
// and returns it. It leaves the cache as it was when the download or the
// write fails; the error wraps ErrSource when the server is at fault.
func (s *Store) Refresh(ctx context.Context) (*Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, modelsdev.DefaultTimeout)
	defer cancel()
	prev := s.cached()
	var etag string
	if prev != nil && len(prev.Data) > 0 {
		etag = prev.ETag
	}
	resp, err := s.fetcher.FetchPath(ctx, DocumentPath, etag)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSource, err)
	}
	data := resp.Data
	if resp.NotModified {
		if etag == "" {
			return nil, fmt.Errorf("%w: it reported no change without a cached copy", ErrSource)
		}
		data = prev.Data
	}
	c, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSource, err)
	}
	now := time.Now()
	if err := s.cache.Store(&modelsdev.Entry{ETag: resp.ETag, Fetched: now, Checked: now, Data: data}); err != nil {
		return nil, err
	}
	return c, nil
}

// cached returns the cached entry, or nil when none is usable. A missing
// cache is expected; any other failure is reported before being treated as
// missing.
func (s *Store) cached() *modelsdev.Entry {
	entry, err := s.cache.Load()
	switch {
	case err == nil:
		return entry
	case errors.Is(err, modelsdev.ErrNoCache):
		return nil
	default:
		slog.Warn("catalog cache unreadable", "path", s.cache.Path(), "err", err)
		return nil
	}
}

// Keep returns a copy of c with only providers ps, and only the models one
// of them offers, and their labs and families, as the Snapshot keeps the
// built-in providers.
func (c *Catalog) Keep(ps ...ProviderID) *Catalog {
	out := &Catalog{Version: c.Version, Source: c.Source, Generated: c.Generated,
		Models: make(map[ModelID]Model), Providers: make(map[ProviderID]Provider)}
	for _, id := range ps {
		if p, ok := c.Providers[id]; ok {
			out.Providers[id] = p
		}
	}
	for id, m := range c.Models {
		m.Providers = slices.DeleteFunc(slices.Clone(m.Providers), func(p ProviderID) bool { return !slices.Contains(ps, p) })
		if len(m.Providers) == 0 {
			continue
		}
		out.Models[id] = m
		if l, ok := c.Labs[m.Lab]; ok {
			if out.Labs == nil {
				out.Labs = make(map[ProviderID]Lab)
			}
			out.Labs[m.Lab] = l
		}
		if f, ok := c.Families[m.Family]; ok {
			if out.Families == nil {
				out.Families = make(map[string]Family)
			}
			out.Families[m.Family] = f
		}
	}
	out.Spellings = c.Spellings.For(out.NameParts())
	return out
}

// PromptWindow returns the most tokens a prompt takes: the input limit when
// the model has one apart from its context window, or else the window.
func (l Limit) PromptWindow() int {
	if l.Input > 0 {
		return l.Input
	}
	return l.Context
}

// Charge returns what r charges, in US dollars, for input tokens read anew,
// cacheRead tokens read from a cache, cacheWrite tokens written to one, and
// output tokens. Cache rates the price does not list are its input rate.
func (r Rates) Charge(input, cacheRead, cacheWrite, output int) float64 {
	read := cmp.Or(r.CacheRead, &r.Input)
	write := cmp.Or(r.CacheWrite, &r.Input)
	return (r.Input*float64(input) + *read*float64(cacheRead) + *write*float64(cacheWrite) + r.Output*float64(output)) / 1_000_000
}

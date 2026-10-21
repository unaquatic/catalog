package modelsdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/unaquatic/catalog/internal/flock"
)

// CacheFile is the catalog cache filename within the application directory.
const CacheFile = "models.json"

// cacheMode is the permission set applied to the cache file. The catalog holds
// no secrets, but the file is private to the user who fetched it.
const cacheMode = 0o600

// ErrNoCache reports that no catalog cache is present.
var ErrNoCache = errors.New("no catalog cache")

// Entry is a cached catalog payload and the metadata needed to revalidate it.
type Entry struct {
	// ETag is the validator returned with the payload, if any.
	ETag string `json:"etag,omitempty"`
	// Fetched is when the source last confirmed the payload.
	Fetched time.Time `json:"fetched"`
	// Checked is when the source was last asked, whether or not it
	// answered. A refresh records it as it starts, so that no other one
	// starts within RetryDelay.
	Checked time.Time `json:"checked,omitzero"`
	// Data is the raw catalog document, empty when no download has
	// succeeded yet.
	Data json.RawMessage `json:"data,omitempty"`
}

// Stale reports whether the entry is older than ttl.
func (e *Entry) Stale(ttl time.Duration) bool {
	return time.Since(e.Fetched) >= ttl
}

// Cache stores a catalog payload in a single file, which is also its lock:
// Load holds a shared lock on it while it reads, and Store and Update an
// exclusive one while they write, so that a reader never sees part of a
// write, in this process or another. The file is rewritten in place, so its
// lock stays on the file that every process opens.
type Cache struct {
	path string
}

// NewCache returns a Cache backed by CacheFile inside dir.
func NewCache(dir string) *Cache {
	return NewCacheFile(dir, CacheFile)
}

// NewCacheFile returns a Cache backed by the file name inside dir, for a
// document other than the catalog, such as flow's own catalog.
func NewCacheFile(dir, name string) *Cache {
	return &Cache{path: filepath.Join(dir, name)}
}

// Path returns the cache file location.
func (c *Cache) Path() string { return c.path }

// Load returns the cached entry. It reports ErrNoCache when the file is absent
// or empty.
func (c *Cache) Load() (*Entry, error) {
	f, err := os.Open(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoCache
	}
	if err != nil {
		return nil, fmt.Errorf("catalog: read cache: %w", err)
	}
	var e *Entry
	err = locked(f, false, func() error {
		e, err = read(f)
		return err
	})
	return e, err
}

// Store writes the entry in place of the cached one.
func (c *Cache) Store(e *Entry) error {
	return c.Update(func(*Entry) (*Entry, error) { return e, nil })
}

// Update replaces the cached entry with the one change returns, given the
// cached entry, or nil when there is none or it cannot be read, all under the
// exclusive lock, so that no other reader or writer comes between. When change
// returns nil, Update writes nothing.
func (c *Cache) Update(change func(*Entry) (*Entry, error)) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("catalog: create cache dir: %w", err)
	}
	f, err := os.OpenFile(c.path, os.O_RDWR|os.O_CREATE, cacheMode)
	if err != nil {
		return fmt.Errorf("catalog: open cache: %w", err)
	}
	return locked(f, true, func() error {
		// An unreadable cache is replaced like a missing one.
		old, _ := read(f)
		e, err := change(old)
		if err != nil || e == nil {
			return err
		}
		data, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("catalog: encode cache: %w", err)
		}
		if _, err := f.WriteAt(data, 0); err != nil {
			return fmt.Errorf("catalog: write cache: %w", err)
		}
		if err := f.Truncate(int64(len(data))); err != nil {
			return fmt.Errorf("catalog: write cache: %w", err)
		}
		return nil
	})
}

// locked runs fn while holding a shared or exclusive lock on f, and closes f
// when done.
func locked(f *os.File, exclusive bool, fn func() error) (err error) {
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := flock.Lock(context.Background(), f, exclusive); err != nil {
		return fmt.Errorf("catalog: lock cache: %w", err)
	}
	defer func() { err = errors.Join(err, flock.Unlock(f)) }()
	return fn()
}

// read returns the entry in f, read from its start, or ErrNoCache when f is
// empty.
func read(f *os.File) (*Entry, error) {
	data, err := io.ReadAll(io.NewSectionReader(f, 0, 1<<62))
	if err != nil {
		return nil, fmt.Errorf("catalog: read cache: %w", err)
	}
	if len(data) == 0 {
		return nil, ErrNoCache
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("catalog: decode cache: %w", err)
	}
	return &e, nil
}

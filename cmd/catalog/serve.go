package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/unaquatic/catalog/gateway"
)

// Loader loads a catalog to serve.
type Loader func(ctx context.Context) (*gateway.Catalog, error)

// LoaderFor returns the loader that o names: one reading the catalog file
// o.Catalog when it is set, and one building the catalog from models.dev,
// as Load does, otherwise.
func LoaderFor(o ServeOptions) Loader {
	if o.Catalog != "" {
		return func(context.Context) (*gateway.Catalog, error) {
			return LoadCatalog(o.Catalog)
		}
	}
	return func(ctx context.Context) (*gateway.Catalog, error) {
		c, rep, err := Load(ctx, o.SourceOptions, time.Now())
		if err == nil {
			slog.Info("catalog built", "source", c.Source, "models", rep.Models, "providers", rep.Providers,
				"listings", rep.Listings, "offerings", rep.Offerings, "variants", rep.Variants, "aliases", rep.AliasIDs,
				"unlisted", rep.Bases[gateway.BasisNone], "services", len(rep.Services), "unknown", len(rep.Unknown), "notes", len(rep.Notes))
		}
		return c, err
	}
}

// Serve loads the catalog as o says and serves it on o.Host and o.Port until
// ctx is done, loading it again every o.Refresh when that is set. It writes
// where it listens to out.
func Serve(ctx context.Context, o ServeOptions, out io.Writer) error {
	load := LoaderFor(o)
	c, err := load(ctx)
	if err != nil {
		return err
	}
	h, err := gateway.NewHandler(c)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if o.Refresh > 0 {
		wg.Go(func() { Refresh(ctx, o.Refresh, load, h) })
	}
	return ListenAndServe(ctx, net.JoinHostPort(o.Host, strconv.Itoa(o.Port)), h, out)
}

// Refresh loads the catalog with load every interval until ctx is done, and
// makes h serve each catalog it loads. A load that fails leaves h serving
// the catalog it has, and is logged.
func Refresh(ctx context.Context, every time.Duration, load Loader, h *gateway.Handler) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		c, err := load(ctx)
		if err == nil {
			err = h.Set(c)
		}
		if err != nil {
			slog.Warn("catalog refresh failed; serving the catalog loaded before", "err", err)
		}
	}
}

// ListenAndServe serves the catalog browser and the endpoints of h, as Site
// combines them, on addr until ctx is done, and writes where it listens to
// out.
func ListenAndServe(ctx context.Context, addr string, h *gateway.Handler, out io.Writer) error {
	srv := &http.Server{Addr: addr, Handler: Site(h), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	c := h.Catalog()
	fmt.Fprintf(out, "catalog browser on http://%s with %d models from %s (Ctrl-C stops it)\n",
		ln.Addr(), len(c.Models), c.Source)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

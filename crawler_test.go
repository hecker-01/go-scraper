package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// runCrawler runs a Crawler to completion and returns the absolute paths of
// every file that was saved (via fileDoneMsg). A 10-second timeout prevents
// tests from hanging if something goes wrong.
func runCrawler(t *testing.T, cfg Config, startURL string) []string {
	t.Helper()
	ch := make(chan tea.Msg, 512)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := NewCrawler(cfg)
	go c.Run(ctx, startURL, ch)

	var paths []string
	for msg := range ch {
		if fd, ok := msg.(fileDoneMsg); ok {
			paths = append(paths, fd.path)
		}
	}
	return paths
}

// savedBasenames returns the base names of all regular files under dir.
func savedBasenames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	names := make(map[string]bool)
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names[filepath.Base(path)] = true
		}
		return nil
	})
	return names
}

// ─── download_media=false ────────────────────────────────────────────────────

// TestDownloadMediaFalse verifies that when download_media is off, CSS and
// images are skipped but HTML pages are saved normally.
func TestDownloadMediaFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w,
				`<html><head><link rel="stylesheet" href="/style.css"></head>`+
					`<body><a href="/about">About</a><img src="/logo.png"></body></html>`)
		case "/about":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>About page</body></html>`)
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprintf(w, `body { color: red; }`)
		case "/logo.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // minimal PNG header bytes
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	outDir := t.TempDir()
	cfg := Config{
		OutputDir:      outDir,
		DownloadMedia:  false,
		DomainDepth:    0,
		MaxDepth:       0,
		MaxMediaSizeMB: 0,
	}

	runCrawler(t, cfg, srv.URL+"/")

	names := savedBasenames(t, outDir)

	// HTML pages must be present.
	for _, want := range []string{"about.html"} {
		if !names[want] {
			t.Errorf("expected HTML file %q to be saved", want)
		}
	}

	// CSS and image must be absent.
	for _, unwanted := range []string{"style.css", "logo.png"} {
		if names[unwanted] {
			t.Errorf("asset %q should not have been saved (download_media=false)", unwanted)
		}
	}
}

// ─── domain_depth=0 ──────────────────────────────────────────────────────────

// TestDomainDepthZero verifies that with domain_depth=0 the crawler stays on
// the starting domain and never fetches URLs from other hosts.
func TestDomainDepthZero(t *testing.T) {
	// External server - should never receive a request with domain_depth=0.
	externalHits := 0
	extSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalHits++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>External page</body></html>`)
	}))
	defer extSrv.Close()

	// Main server - its root page links to the external server.
	mainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w,
			`<html><body>`+
				`<a href="/local">Local</a>`+
				`<a href="%s/page">External</a>`+
				`</body></html>`, extSrv.URL)
	}))
	defer mainSrv.Close()

	outDir := t.TempDir()
	cfg := Config{
		OutputDir:      outDir,
		DownloadMedia:  true,
		DomainDepth:    0, // must not leave the starting host
		MaxDepth:       0,
		MaxMediaSizeMB: 0,
	}

	runCrawler(t, cfg, mainSrv.URL+"/")

	if externalHits > 0 {
		t.Errorf("external server was hit %d time(s) despite domain_depth=0", externalHits)
	}
}

// ─── max_media_size_mb ────────────────────────────────────────────────────────

// TestMaxMediaSizeMB verifies that files exceeding the configured cap are
// skipped while smaller files (and HTML) are saved normally.
func TestMaxMediaSizeMB(t *testing.T) {
	const capMB = 1
	smallImg := make([]byte, 512*1024)   // 0.5 MB - under cap
	largeImg := make([]byte, 2*1024*1024) // 2 MB - over cap

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w,
				`<html><body>`+
					`<img src="/small.png">`+
					`<img src="/big.png">`+
					`</body></html>`)
		case "/small.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(smallImg)))
			w.Write(smallImg)
		case "/big.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeImg)))
			w.Write(largeImg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	outDir := t.TempDir()
	cfg := Config{
		OutputDir:      outDir,
		DownloadMedia:  true,
		DomainDepth:    0,
		MaxDepth:       0,
		MaxMediaSizeMB: capMB,
	}

	runCrawler(t, cfg, srv.URL+"/")

	names := savedBasenames(t, outDir)

	// Small image must be saved.
	if !names["small.png"] {
		t.Error("small.png (under cap) should have been saved")
	}

	// Large image must not be saved.
	if names["big.png"] {
		t.Errorf("big.png (over %d MB cap) should not have been saved", capMB)
	}
}

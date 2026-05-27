package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ─── Message types ────────────────────────────────────────────────────────────

// logLineMsg is sent on the output channel for each log event.
type logLineMsg struct {
	line  string
	level slog.Level
}

// fileDoneMsg is sent on the output channel each time a file is saved.
type fileDoneMsg struct {
	path string
	size int64
}

// crawlDoneMsg is sent when the crawl finishes (success, error, or cancel).
type crawlDoneMsg struct {
	treeOutput string
	totalBytes int64
	err        error
}

// waitForOutput returns a Cmd that blocks until the next message on ch.
// Same pattern as twdl-go.
func waitForOutput(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// ─── DownloadResult ───────────────────────────────────────────────────────────

// DownloadResult holds the outcome of a single file download.
type DownloadResult struct {
	Path        string // absolute path to the saved file
	ContentType string // normalised MIME type (e.g. "text/html")
	Bytes       int64
}

// ─── Downloader ───────────────────────────────────────────────────────────────

// Downloader fetches URLs and saves them to disk according to the app config.
// Construct with NewDownloader; do not use the zero value.
type Downloader struct {
	cfg    Config
	client *http.Client
}

// NewDownloader returns a Downloader ready for use.
func NewDownloader(cfg Config) *Downloader {
	return &Downloader{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Download fetches rawURL and saves it to the configured output directory.
// The local file path is derived from the URL and the response Content-Type
// (see URLToLocalPath in rewriter.go for the naming rules).
//
// ctx is forwarded to the HTTP request - cancelling it aborts the download.
// Returns a DownloadResult on success. The ContentType field lets the caller
// (Crawler) decide whether to parse the response as HTML.
func (d *Downloader) Download(ctx context.Context, rawURL string) (DownloadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("building request for %s: %w", rawURL, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	ct := normaliseContentType(resp.Header.Get("Content-Type"))

	// Fast-path size check via Content-Length (avoids streaming at all).
	if err := d.checkSizeCap(ct, resp.ContentLength); err != nil {
		return DownloadResult{ContentType: ct}, err
	}

	// Resolve the local file path now that we know the content-type.
	rel, err := URLToLocalPath(rawURL, ct)
	if err != nil {
		return DownloadResult{ContentType: ct}, fmt.Errorf("resolving path for %s: %w", rawURL, err)
	}
	destPath := filepath.Join(expandHome(d.cfg.OutputDir), rel)

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return DownloadResult{ContentType: ct}, fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return DownloadResult{ContentType: ct}, fmt.Errorf("create %s: %w", destPath, err)
	}

	// Stream with optional cap enforcement.
	reader, limitBytes := d.limitedReader(resp.Body, ct)
	n, copyErr := io.Copy(f, reader)
	f.Close()

	if copyErr != nil {
		os.Remove(destPath)
		return DownloadResult{ContentType: ct}, fmt.Errorf("writing %s: %w", destPath, copyErr)
	}
	// If we read one byte past the limit the file exceeded the cap.
	if limitBytes > 0 && n > limitBytes {
		os.Remove(destPath)
		return DownloadResult{ContentType: ct}, fmt.Errorf(
			"skipped %s: exceeds %d MB cap", rawURL, d.cfg.MaxMediaSizeMB,
		)
	}

	return DownloadResult{Path: destPath, ContentType: ct, Bytes: n}, nil
}

// checkSizeCap returns an error if Content-Length already exceeds the cap.
// Only applies to media files (not HTML pages). contentLength < 0 means unknown.
func (d *Downloader) checkSizeCap(contentType string, contentLength int64) error {
	if d.cfg.MaxMediaSizeMB <= 0 || !isMediaContent(contentType) {
		return nil
	}
	limit := int64(d.cfg.MaxMediaSizeMB) << 20
	if contentLength > limit {
		return fmt.Errorf(
			"skipped: %.1f MB exceeds %d MB cap",
			float64(contentLength)/(1<<20), d.cfg.MaxMediaSizeMB,
		)
	}
	return nil
}

// limitedReader wraps r with an io.LimitReader when the size cap applies.
// Returns the (possibly wrapped) reader and the byte limit (0 = no limit).
func (d *Downloader) limitedReader(r io.Reader, contentType string) (io.Reader, int64) {
	if d.cfg.MaxMediaSizeMB <= 0 || !isMediaContent(contentType) {
		return r, 0
	}
	limit := int64(d.cfg.MaxMediaSizeMB) << 20
	// Read one byte past the limit so we can detect an overrun.
	return io.LimitReader(r, limit+1), limit
}

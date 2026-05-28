package main

import (
	"path/filepath"
	"testing"
)

// ─── formatBytes ─────────────────────────────────────────────────────────────

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
		{int64(1.5 * (1 << 20)), "1.5 MB"},
		{1 << 30, "1.0 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.n)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// ─── isValidURL ──────────────────────────────────────────────────────────────

func TestIsValidURL(t *testing.T) {
	valid := []string{
		"http://example.com",
		"https://example.com",
		"https://example.com/path?q=1",
		"example.com",
		"heckr.dev",
		"sub.example.co.uk",
	}
	invalid := []string{
		"",
		"ftp://example.com",
		"javascript:void(0)",
		"  ",
		"notadomain",
	}
	for _, u := range valid {
		if !isValidURL(u) {
			t.Errorf("isValidURL(%q) should be true", u)
		}
	}
	for _, u := range invalid {
		if isValidURL(u) {
			t.Errorf("isValidURL(%q) should be false", u)
		}
	}
}

func TestAddScheme(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"heckr.dev", "https://heckr.dev"},
		{"example.com/path", "https://example.com/path"},
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"},
		{"  heckr.dev  ", "https://heckr.dev"},
		{"", ""},
	}
	for _, tt := range tests {
		got := addScheme(tt.in)
		if got != tt.want {
			t.Errorf("addScheme(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ─── normaliseContentType ────────────────────────────────────────────────────

func TestNormaliseContentType(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"text/html; charset=utf-8", "text/html"},
		{"TEXT/HTML; CHARSET=UTF-8", "text/html"},
		{"image/png", "image/png"},
		{"application/javascript", "application/javascript"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normaliseContentType(tt.raw)
		if got != tt.want {
			t.Errorf("normaliseContentType(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// ─── isHTMLContentType ───────────────────────────────────────────────────────

func TestIsHTMLContentType(t *testing.T) {
	html := []string{
		"text/html",
		"text/html; charset=utf-8",
		"application/xhtml+xml",
	}
	notHTML := []string{
		"text/css",
		"application/javascript",
		"image/png",
		"",
	}
	for _, ct := range html {
		if !isHTMLContentType(ct) {
			t.Errorf("isHTMLContentType(%q) should be true", ct)
		}
	}
	for _, ct := range notHTML {
		if isHTMLContentType(ct) {
			t.Errorf("isHTMLContentType(%q) should be false", ct)
		}
	}
}

// ─── isMediaContent ──────────────────────────────────────────────────────────

func TestIsMediaContent(t *testing.T) {
	media := []string{
		"text/css",
		"application/javascript",
		"image/png",
		"image/webp",
		"video/mp4",
		"font/woff2",
	}
	notMedia := []string{
		"text/html",
		"text/html; charset=utf-8",
		"application/xhtml+xml",
	}
	for _, ct := range media {
		if !isMediaContent(ct) {
			t.Errorf("isMediaContent(%q) should be true", ct)
		}
	}
	for _, ct := range notMedia {
		if isMediaContent(ct) {
			t.Errorf("isMediaContent(%q) should be false", ct)
		}
	}
}

// ─── isMediaURL ──────────────────────────────────────────────────────────────

func TestIsMediaURL(t *testing.T) {
	media := []string{
		"https://example.com/style.css",
		"https://example.com/app.js",
		"https://example.com/img/logo.png",
		"https://example.com/font.woff2",
		"https://example.com/video.mp4",
		"https://example.com/icon.ico",
	}
	notMedia := []string{
		"https://example.com/",
		"https://example.com/about",
		"https://example.com/api/data",
	}
	for _, u := range media {
		if !isMediaURL(u) {
			t.Errorf("isMediaURL(%q) should be true", u)
		}
	}
	for _, u := range notMedia {
		if isMediaURL(u) {
			t.Errorf("isMediaURL(%q) should be false", u)
		}
	}
}

// ─── extForContentType ───────────────────────────────────────────────────────

func TestExtForContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want string
	}{
		{"text/html", ".html"},
		{"text/html; charset=utf-8", ".html"},
		{"application/xhtml+xml", ".html"},
		{"text/css", ".css"},
		{"application/javascript", ".js"},
		{"text/javascript", ".js"},
		{"application/json", ".json"},
		{"text/plain", ".txt"},
		{"application/xml", ".xml"},
		{"text/xml", ".xml"},
		{"image/svg+xml", ".svg"},
		// Unknown types produce no extension.
		{"image/png", ""},
		{"video/mp4", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extForContentType(tt.ct)
		if got != tt.want {
			t.Errorf("extForContentType(%q) = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

// ─── URLToLocalPath ───────────────────────────────────────────────────────────

func TestURLToLocalPath(t *testing.T) {
	tests := []struct {
		url  string
		ct   string
		want string // uses filepath.Join so separators are OS-correct
	}{
		// Root of site -> host/index.html
		{
			"https://example.com/",
			"text/html",
			filepath.Join("example.com", "index.html"),
		},
		// Extension-less path -> append extension from Content-Type
		{
			"https://example.com/about",
			"text/html",
			filepath.Join("example.com", "about.html"),
		},
		{
			"https://example.com/api/data",
			"application/json",
			filepath.Join("example.com", "api", "data.json"),
		},
		// Trailing slash -> index.html inside the directory
		{
			"https://example.com/about/",
			"text/html",
			filepath.Join("example.com", "about", "index.html"),
		},
		// Has extension -> keep as-is
		{
			"https://example.com/style.css",
			"text/css",
			filepath.Join("example.com", "style.css"),
		},
		{
			"https://example.com/img/logo.png",
			"image/png",
			filepath.Join("example.com", "img", "logo.png"),
		},
		// Query string is stripped (does not affect the saved path)
		{
			"https://example.com/search?q=go&page=2",
			"text/html",
			filepath.Join("example.com", "search.html"),
		},
		// Unknown Content-Type -> no extension appended
		{
			"https://example.com/download",
			"",
			filepath.Join("example.com", "download"),
		},
	}
	for _, tt := range tests {
		got, err := URLToLocalPath(tt.url, tt.ct)
		if err != nil {
			t.Errorf("URLToLocalPath(%q, %q) unexpected error: %v", tt.url, tt.ct, err)
			continue
		}
		if got != tt.want {
			t.Errorf("URLToLocalPath(%q, %q)\n  got  %q\n  want %q", tt.url, tt.ct, got, tt.want)
		}
	}
}

// ─── parseSrcset ─────────────────────────────────────────────────────────────

func TestParseSrcset(t *testing.T) {
	tests := []struct {
		srcset string
		want   []string
	}{
		{"", nil},
		{"image.png", []string{"image.png"}},
		{"image.png 2x", []string{"image.png"}},
		{"small.png 300w, large.png 600w", []string{"small.png", "large.png"}},
		{"  img.jpg ,  img@2x.jpg 2x  ", []string{"img.jpg", "img@2x.jpg"}},
	}
	for _, tt := range tests {
		got := parseSrcset(tt.srcset)
		if len(got) != len(tt.want) {
			t.Errorf("parseSrcset(%q) len=%d want len=%d: got %v want %v",
				tt.srcset, len(got), len(tt.want), got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("parseSrcset(%q)[%d] = %q, want %q", tt.srcset, i, got[i], tt.want[i])
			}
		}
	}
}

// ─── normaliseURL ────────────────────────────────────────────────────────────

func TestNormaliseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://example.com/page", "https://example.com/page"},
		// Fragment is stripped.
		{"https://example.com/page#section", "https://example.com/page"},
		{"http://example.com/", "http://example.com/"},
		// Non-http schemes return "".
		{"ftp://example.com/", ""},
		{"javascript:void(0)", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := normaliseURL(tt.raw)
		if got != tt.want {
			t.Errorf("normaliseURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// ─── extractDomain ───────────────────────────────────────────────────────────

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// Standard URL - no port in result.
		{"https://example.com/page", "example.com"},
		// Sub-domain is preserved.
		{"http://sub.example.com/", "sub.example.com"},
		// Explicit non-standard port is included.
		{"http://example.com:8080/", "example.com:8080"},
		// Two servers on the same IP but different ports are different domains.
		{"http://127.0.0.1:9001/", "127.0.0.1:9001"},
		{"http://127.0.0.1:9002/", "127.0.0.1:9002"},
		// Bad URL returns "".
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		got := extractDomain(tt.url)
		if got != tt.want {
			t.Errorf("extractDomain(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

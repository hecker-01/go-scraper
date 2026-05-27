package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// formatBytes formats a byte count into a human-readable string.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// expandHome replaces a leading ~/ with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + "/" + path[2:]
		}
	}
	return path
}

// isValidURL returns true if s looks like an http or https URL.
func isValidURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// parseBool converts common yes/no strings to a bool.
// Returns fallback if the input is not recognised.
func parseBool(s string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "true", "1":
		return true
	case "n", "no", "false", "0":
		return false
	}
	return fallback
}

// boolToStr returns "true" or "false" for display in the config wizard.
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// normaliseContentType strips parameters from a Content-Type header value and
// lowercases the result. "text/html; charset=utf-8" -> "text/html".
func normaliseContentType(raw string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]))
}

// isHTMLContentType returns true when the content-type indicates an HTML page.
func isHTMLContentType(ct string) bool {
	ct = normaliseContentType(ct)
	return strings.HasPrefix(ct, "text/html") ||
		strings.HasPrefix(ct, "application/xhtml")
}

// isMediaURL does a quick extension-based check on a URL to decide if it is
// a non-HTML asset. Used by the Crawler to respect the DownloadMedia setting
// before even making a request. CSS and JS are included because when
// download_media is false the user wants only HTML pages.
func isMediaURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	mediaExts := []string{
		// stylesheets and scripts
		".css", ".js",
		// images
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".svg",
		// video / audio
		".mp4", ".webm", ".mov", ".avi", ".mp3", ".ogg", ".wav",
		// fonts
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		// archives / documents
		".pdf", ".zip",
	}
	for _, ext := range mediaExts {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

// isJSContentType returns true when the content-type indicates a JavaScript file.
func isJSContentType(ct string) bool {
	return strings.Contains(normaliseContentType(ct), "javascript")
}

// isMediaContent returns true for any content-type that is not an HTML page.
// Used to decide whether the size cap (MaxMediaSizeMB) applies to a download.
// HTML is always fetched regardless of size because it is the content we
// parse for links; everything else (images, CSS, JS, fonts, video, etc.) is
// subject to the cap.
func isMediaContent(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return !strings.HasPrefix(ct, "text/html") &&
		!strings.HasPrefix(ct, "application/xhtml")
}

// extractDomain returns the host (including port if non-standard) from rawURL,
// or an empty string on error. Including the port means that two servers on
// different ports of the same IP are treated as different domains - which is
// the correct behaviour both for testing and for real multi-port deployments.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host // includes port when explicitly present in the URL
}

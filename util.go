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

// boolToYesNo returns "yes" or "no" for display in the config wizard.
func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
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
// likely a media file (image, font, audio, video). Used by the Crawler to
// respect the DownloadMedia setting before even making a request.
func isMediaURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	mediaExts := []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".ico", ".svg",
		".mp4", ".webm", ".mov", ".avi", ".mp3", ".ogg", ".wav",
		".woff", ".woff2", ".ttf", ".otf", ".eot",
		".pdf", ".zip",
	}
	for _, ext := range mediaExts {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
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

// extractDomain returns the hostname from rawURL, or an empty string on error.
// Used by the crawler to track domain hops for the domain_depth config.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

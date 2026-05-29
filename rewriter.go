package main

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// ─── URLToLocalPath ───────────────────────────────────────────────────────────

// URLToLocalPath converts an absolute URL to the relative filesystem path used
// when saving the file. Query strings and fragments are stripped because they
// do not map to files on disk.
//
// Path naming rules:
//
//   - Has a file extension (e.g. style.css, logo.png, page.html)
//     -> saved as-is under the host directory
//
//   - No extension AND no trailing slash (e.g. /about, /docs/guide)
//     -> extension derived from contentType (e.g. /about + text/html -> about.html)
//     -> if contentType is unknown or unrecognised, saved with no extension
//
//   - Explicit trailing slash (e.g. /docs/, /blog/) or root /
//     -> server is signalling "directory"; saved as index.html inside that directory
//
// Examples:
//
//	"https://example.com/"             ct="text/html"  -> "example.com/index.html"
//	"https://example.com/about"        ct="text/html"  -> "example.com/about.html"
//	"https://example.com/about/"       ct="text/html"  -> "example.com/about/index.html"
//	"https://example.com/style.css"    ct="text/css"   -> "example.com/style.css"
//	"https://example.com/img/logo.png" ct="image/png"  -> "example.com/img/logo.png"
//	"https://example.com/api/data"     ct="application/json" -> "example.com/api/data.json"
func URLToLocalPath(rawURL, contentType string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	p := path.Clean(u.Path)
	if p == "." {
		p = "/"
	}

	trailingSlash := strings.HasSuffix(u.Path, "/")
	base := path.Base(p)
	hasExt := strings.Contains(base, ".") && base != "."

	var localPath string
	switch {
	case p == "/" || trailingSlash:
		// Root or explicit directory URL - save as index.html inside the directory,
		// matching what a real web server would serve.
		localPath = filepath.Join(u.Host, filepath.FromSlash(p), "index.html")

	case hasExt:
		// Use the content-type extension when known so that server-side scripts
		// (e.g. index.php served as text/html) are saved with the correct extension.
		// Fall back to the URL's original extension for unrecognised types (images, etc.).
		if ctExt := extForContentType(contentType); ctExt != "" {
			stripped := strings.TrimSuffix(p, path.Ext(p))
			localPath = filepath.Join(u.Host, filepath.FromSlash(stripped)+ctExt)
		} else {
			localPath = filepath.Join(u.Host, filepath.FromSlash(p))
		}

	default:
		// No extension, no trailing slash - derive extension from Content-Type
		// so the saved file reflects what was actually served.
		ext := extForContentType(contentType)
		localPath = filepath.Join(u.Host, filepath.FromSlash(p)+ext)
	}

	return localPath, nil
}

// extForContentType maps a normalised MIME type to a file extension.
// Returns an empty string for unrecognised or missing types.
func extForContentType(ct string) string {
	// Strip parameters: "text/html; charset=utf-8" -> "text/html"
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch ct {
	case "text/html", "application/xhtml+xml":
		return ".html"
	case "text/css":
		return ".css"
	case "application/javascript", "text/javascript":
		return ".js"
	case "application/json":
		return ".json"
	case "text/plain":
		return ".txt"
	case "application/xml", "text/xml":
		return ".xml"
	case "image/svg+xml":
		return ".svg"
	default:
		return ""
	}
}

// ─── Rewriter ─────────────────────────────────────────────────────────────────

// Rewriter rewrites href and src attributes in saved HTML files so that
// absolute URLs are replaced with relative local paths, making the downloaded
// site work offline in a browser.
type Rewriter struct {
	outputDir string
}

// NewRewriter returns a Rewriter for the given output directory.
func NewRewriter(outputDir string) *Rewriter {
	return &Rewriter{outputDir: outputDir}
}

// RewriteLinks opens htmlPath, rewrites all absolute href / src / srcset
// attributes to relative local paths, and overwrites the file with the
// updated HTML.
//
// baseURL is the original URL of the page (used to resolve relative links).
// savedPaths maps normalised absolute URL -> absolute local path for every
// file downloaded so far; only URLs present in the map are rewritten.
func (r *Rewriter) RewriteLinks(htmlPath, baseURL string, savedPaths map[string]string) error {
	f, err := os.Open(htmlPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", htmlPath, err)
	}
	doc, err := html.Parse(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("parsing HTML %s: %w", htmlPath, err)
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parsing base URL %s: %w", baseURL, err)
	}

	r.rewriteTree(doc, base, htmlPath, savedPaths)

	out, err := os.Create(htmlPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", htmlPath, err)
	}
	defer out.Close()

	return html.Render(out, doc)
}

// ─── tree traversal ───────────────────────────────────────────────────────────

// rewriteTree recursively walks the HTML node tree, rewriting URL attributes
// on every element node it encounters.
func (r *Rewriter) rewriteTree(n *html.Node, base *url.URL, htmlPath string, savedPaths map[string]string) {
	if n.Type == html.ElementNode {
		r.rewriteElementAttrs(n, base, htmlPath, savedPaths)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.rewriteTree(c, base, htmlPath, savedPaths)
	}
}

// rewriteElementAttrs patches the URL-bearing attributes on a single element.
// Mirrors the element list in Crawler.linksFromNode so that the same URLs
// that were enqueued are also rewritten.
func (r *Rewriter) rewriteElementAttrs(n *html.Node, base *url.URL, htmlPath string, savedPaths map[string]string) {
	switch n.Data {
	case "a", "link":
		r.rewriteAttr(n, "href", base, htmlPath, savedPaths)
	case "script", "iframe":
		r.rewriteAttr(n, "src", base, htmlPath, savedPaths)
	case "img", "video", "audio", "source", "track", "embed":
		r.rewriteAttr(n, "src", base, htmlPath, savedPaths)
		r.rewriteSrcsetAttr(n, base, htmlPath, savedPaths)
	}
}

// ─── attribute helpers ────────────────────────────────────────────────────────

// rewriteAttr replaces the value of attribute key on node n with the
// relative local path, if the URL was downloaded.
func (r *Rewriter) rewriteAttr(n *html.Node, key string, base *url.URL, htmlPath string, savedPaths map[string]string) {
	for i, a := range n.Attr {
		if a.Key != key {
			continue
		}
		if rel := r.resolveToLocal(a.Val, base, htmlPath, savedPaths); rel != "" {
			n.Attr[i].Val = rel
		}
		return
	}
}

// rewriteSrcsetAttr rewrites every URL inside a srcset attribute value,
// keeping any width/density descriptors intact.
func (r *Rewriter) rewriteSrcsetAttr(n *html.Node, base *url.URL, htmlPath string, savedPaths map[string]string) {
	for i, a := range n.Attr {
		if a.Key != "srcset" {
			continue
		}
		if newVal := r.rewriteSrcset(a.Val, base, htmlPath, savedPaths); newVal != "" {
			n.Attr[i].Val = newVal
		}
		return
	}
}

// rewriteSrcset rewrites all URLs inside a srcset value. Each comma-separated
// entry is "<url> [descriptor]", e.g. "image@2x.png 2x". Only the URL token
// is replaced; the descriptor is preserved as-is.
func (r *Rewriter) rewriteSrcset(srcset string, base *url.URL, htmlPath string, savedPaths map[string]string) string {
	var parts []string
	changed := false
	for _, entry := range strings.Split(srcset, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		fields := strings.Fields(entry)
		if len(fields) == 0 {
			continue
		}
		if rel := r.resolveToLocal(fields[0], base, htmlPath, savedPaths); rel != "" {
			fields[0] = rel
			changed = true
		}
		parts = append(parts, strings.Join(fields, " "))
	}
	if !changed || len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

// resolveToLocal resolves rawAttr against base, looks it up in savedPaths,
// and returns a slash-separated relative path from htmlPath's directory to
// the saved file. Returns "" if the URL was not downloaded or cannot be resolved.
func (r *Rewriter) resolveToLocal(rawAttr string, base *url.URL, htmlPath string, savedPaths map[string]string) string {
	abs, err := resolveURL(base, rawAttr)
	if err != nil || abs == "" {
		return ""
	}
	norm := normaliseURL(abs)
	if norm == "" {
		return ""
	}
	targetPath, ok := savedPaths[norm]
	if !ok {
		return ""
	}
	rel, err := filepath.Rel(filepath.Dir(htmlPath), targetPath)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"golang.org/x/net/html"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Crawler ──────────────────────────────────────────────────────────────────

// htmlEntry records an HTML file that needs link rewriting after the crawl.
type htmlEntry struct {
	path string // absolute local path to the saved HTML file
	url  string // original URL of the page (for resolving relative links)
}

// Crawler manages the recursive crawl: it maintains the URL queue, tracks
// visited URLs, enforces page-depth and domain-depth limits, and streams
// progress messages back to the Bubbletea model via a channel.
type Crawler struct {
	cfg         Config
	downloader  *Downloader
	rewriter    *Rewriter
	visited     map[string]bool
	startDomain string
	savedPaths  map[string]string // normalised URL -> absolute local path
	htmlFiles   []htmlEntry        // HTML pages queued for link rewriting
}

// NewCrawler returns a Crawler ready to run.
func NewCrawler(cfg Config) *Crawler {
	return &Crawler{
		cfg:        cfg,
		downloader: NewDownloader(cfg),
		rewriter:   NewRewriter(expandHome(cfg.OutputDir)),
		visited:    make(map[string]bool),
		savedPaths: make(map[string]string),
	}
}

// ─── queueItem ────────────────────────────────────────────────────────────────

// queueItem holds a URL together with how deep in the page graph it was found
// and how many domain boundaries have been crossed to reach it.
type queueItem struct {
	url        string
	pageDepth  int // hops from the start URL through page links
	domainHops int // number of times the domain changed since the start URL
}

// ─── crawlStartedMsg ─────────────────────────────────────────────────────────

// crawlStartedMsg hands the output channel and cancel func to the model.
type crawlStartedMsg struct {
	ch     <-chan tea.Msg
	cancel func()
}

// ─── startCrawl (tea.Cmd entry point) ────────────────────────────────────────

// startCrawl returns a tea.Cmd that spins up the crawl goroutine and
// immediately returns a crawlStartedMsg so the model can start listening.
// The cancel func is wired to a real context - calling it stops the goroutine
// cleanly at the next queue iteration or mid-download via request cancellation.
func startCrawl(startURL string, cfg Config) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan tea.Msg, 256)
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			c := NewCrawler(cfg)
			c.Run(ctx, startURL, ch)
		}()
		return crawlStartedMsg{ch: ch, cancel: cancel}
	}
}

// ─── Run ──────────────────────────────────────────────────────────────────────

// Run executes the full recursive crawl starting at startURL.
// It sends fileDoneMsg, logLineMsg, and a final crawlDoneMsg on ch, then
// closes ch when done. Cancelling ctx stops the crawl after the current
// item finishes and sends a partial crawlDoneMsg before returning.
func (c *Crawler) Run(ctx context.Context, startURL string, ch chan<- tea.Msg) {
	defer close(ch)

	c.startDomain = extractDomain(startURL)
	queue := []queueItem{{url: startURL, pageDepth: 0, domainHops: 0}}

	for len(queue) > 0 {
		// Check for cancellation before starting each new item.
		select {
		case <-ctx.Done():
			c.rewriteAllHTML(ch)
			reporter := NewReporter(expandHome(c.cfg.OutputDir))
			tree, totalBytes, _ := reporter.Build()
			ch <- crawlDoneMsg{treeOutput: tree, totalBytes: totalBytes}
			return
		default:
		}

		item := queue[0]
		queue = queue[1:]

		if c.visited[item.url] {
			continue
		}
		c.visited[item.url] = true

		links, err := c.processURL(ctx, item, ch)
		if err != nil {
			ch <- logLineMsg{line: err.Error(), level: slog.LevelWarn}
			continue
		}

		for _, link := range links {
			if c.visited[link.url] {
				continue
			}
			if c.cfg.MaxDepth > 0 && link.pageDepth > c.cfg.MaxDepth {
				continue
			}
			if link.domainHops > c.cfg.DomainDepth {
				continue
			}
			queue = append(queue, link)
		}
	}

	// Rewrite links in all HTML files now that every file is on disk.
	c.rewriteAllHTML(ch)

	// Normal completion - build final report.
	reporter := NewReporter(expandHome(c.cfg.OutputDir))
	tree, totalBytes, _ := reporter.Build()
	ch <- crawlDoneMsg{treeOutput: tree, totalBytes: totalBytes}
}

// ─── processURL ───────────────────────────────────────────────────────────────

// processURL downloads a single URL, emits progress messages, and - if the
// response is HTML - returns the links found on the page as new queue items.
// ctx is forwarded to the HTTP layer so cancellation aborts in-flight downloads.
func (c *Crawler) processURL(ctx context.Context, item queueItem, ch chan<- tea.Msg) ([]queueItem, error) {
	// Honour the download_media setting without making a request.
	if !c.cfg.DownloadMedia && isMediaURL(item.url) {
		return nil, nil
	}

	result, err := c.downloader.Download(ctx, item.url)
	if err != nil {
		return nil, err
	}

	// Record the URL -> local path mapping for the link rewriter.
	c.savedPaths[normaliseURL(item.url)] = result.Path

	ch <- fileDoneMsg{path: result.Path, size: result.Bytes}
	ch <- logLineMsg{line: result.Path, level: slog.LevelInfo}

	if !isHTMLContentType(result.ContentType) {
		return nil, nil
	}

	// Queue this page for link rewriting after the full crawl completes.
	c.htmlFiles = append(c.htmlFiles, htmlEntry{path: result.Path, url: item.url})

	// Extract links BEFORE rewriting so absolute URLs are still present.
	return c.extractLinks(result.Path, item.url, item.pageDepth, item.domainHops)
}

// ─── extractLinks ─────────────────────────────────────────────────────────────

// extractLinks opens the saved HTML file at htmlPath, traverses the node tree,
// and returns a queue item for every unique, crawlable URL found.
// pageURL is the original URL of the page (needed to resolve relative hrefs).
func (c *Crawler) extractLinks(htmlPath, pageURL string, pageDepth, domainHops int) ([]queueItem, error) {
	f, err := os.Open(htmlPath)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", htmlPath, err)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("parsing HTML %s: %w", htmlPath, err)
	}

	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL %s: %w", pageURL, err)
	}

	// seenOnPage deduplicates links within this one page before they reach
	// the global visited map.
	seenOnPage := make(map[string]bool)
	var items []queueItem

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, raw := range c.linksFromNode(n) {
				abs, err := resolveURL(base, raw)
				if err != nil || abs == "" {
					continue
				}
				norm := normaliseURL(abs)
				if norm == "" || seenOnPage[norm] || c.visited[norm] {
					continue
				}
				seenOnPage[norm] = true

				hops := domainHops
				if extractDomain(norm) != c.startDomain {
					hops++
				}

				items = append(items, queueItem{
					url:        norm,
					pageDepth:  pageDepth + 1,
					domainHops: hops,
				})
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}
	traverse(doc)

	return items, nil
}

// ─── linksFromNode ────────────────────────────────────────────────────────────

// linksFromNode returns all URL strings worth following from a single HTML node.
// It covers href, src, and srcset attributes across the common HTML elements.
func (c *Crawler) linksFromNode(n *html.Node) []string {
	var urls []string

	switch n.Data {
	case "a", "link":
		if v := attrVal(n, "href"); v != "" {
			urls = append(urls, v)
		}
	case "script", "iframe":
		if v := attrVal(n, "src"); v != "" {
			urls = append(urls, v)
		}
	case "img", "video", "audio", "source", "track", "embed":
		if v := attrVal(n, "src"); v != "" {
			urls = append(urls, v)
		}
		// srcset carries multiple space-separated URLs with optional descriptors.
		if v := attrVal(n, "srcset"); v != "" {
			urls = append(urls, parseSrcset(v)...)
		}
	}

	return urls
}

// ─── rewriteAllHTML ───────────────────────────────────────────────────────────

// rewriteAllHTML rewrites links in every HTML file collected during the crawl.
// It is called once after the queue is drained so that all target files are
// already on disk when the relative paths are computed. Errors are non-fatal:
// a warning is sent on ch and the next file is tried.
func (c *Crawler) rewriteAllHTML(ch chan<- tea.Msg) {
	for _, entry := range c.htmlFiles {
		if err := c.rewriter.RewriteLinks(entry.path, entry.url, c.savedPaths); err != nil {
			ch <- logLineMsg{
				line:  fmt.Sprintf("rewrite %s: %s", entry.path, err.Error()),
				level: slog.LevelWarn,
			}
		}
	}
}

// ─── HTML / URL helpers ───────────────────────────────────────────────────────

// attrVal returns the trimmed value of attribute key on node n, or "".
func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// parseSrcset splits a srcset attribute value and returns only the URL tokens.
// Each entry is "<url> [descriptor]", e.g. "image@2x.png 2x" or "img.png 300w".
func parseSrcset(srcset string) []string {
	var urls []string
	for _, part := range strings.Split(srcset, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// The URL is always the first whitespace-separated token.
		if fields := strings.Fields(part); len(fields) > 0 {
			urls = append(urls, fields[0])
		}
	}
	return urls
}

// resolveURL resolves href against base into an absolute URL string.
// Returns ("", nil) for empty hrefs, fragment-only links, and non-http schemes
// such as javascript:, mailto:, data:, tel:.
func resolveURL(base *url.URL, href string) (string, error) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return "", nil
	}
	lower := strings.ToLower(href)
	for _, skip := range []string{"javascript:", "mailto:", "data:", "tel:", "ftp:"} {
		if strings.HasPrefix(lower, skip) {
			return "", nil
		}
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

// normaliseURL strips the fragment from rawURL and returns the cleaned string
// for use as a queue / visited-map key.
// Returns "" for anything that is not http or https.
func normaliseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

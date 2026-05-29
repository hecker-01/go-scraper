# go-scraper

A terminal UI tool that downloads a full website to a local folder.
It crawls recursively, rewrites links for offline browsing, and shows a live progress view while it works.

---

## Features

- Recursive crawl with visited-URL deduplication (no infinite loops)
- Downloads HTML, images, CSS, JS, fonts and other media
- Rewrites all href/src/srcset links to relative local paths so the site works offline
- Files saved with the extension the server actually serves (e.g. `index.php` served as HTML → `index.html`)
- Built-in HTTP server to browse downloaded sites in your browser
- Live TUI: spinner, file count, bytes downloaded, last filename
- Error log shown in-app (warnings in red, older ones summarised)
- File tree + total size summary on the done screen
- Clickable folder link in the terminal (opens in Finder on macOS, Explorer on Windows)
- TOML config with a first-boot wizard and `Ctrl+G` shortcut to re-open it
- Respects `domain_depth` (stay on one domain or follow N hops to external sites)
- Optional media size cap (`max_media_size_mb`) so large videos do not fill your disk

---

## Install

**Requirements:** Go 1.22 or newer

```sh
git clone https://github.com/hecker-01/go-scraper
cd go-scraper
make install
```

`make install` builds the binary and copies it to `/usr/local/bin` (macOS/Linux) or `C:\Windows\System32` (Windows), elevating to `sudo` automatically if the destination requires it.

If you prefer to install manually, run `make build` and move the binary yourself:

**macOS / Linux:**
```sh
mv go-scraper /usr/local/bin/
```

**Windows** (run in PowerShell as Administrator):
```powershell
Move-Item go-scraper.exe C:\Windows\System32\go-scraper.exe
```

Or move it to any folder already on your `%PATH%`, for example `C:\Program Files\go-scraper\`, and add that folder via **System Properties → Environment Variables**.

---

## Usage

```sh
go-scraper                              # open TUI, type a URL interactively
go-scraper --url https://example.com   # pre-fill URL and start crawling immediately
go-scraper --setup                     # open the config wizard directly
go-scraper --serve                     # serve the output directory over HTTP
go-scraper --version                   # print version and exit
go-scraper --help                      # show help and exit
```

`--serve` requires `serve_port` to be set in your config (run `--setup` or press `Ctrl+G` in the TUI to configure it). It serves the full output directory so you can browse any previously downloaded site at `http://localhost:<port>/<domain>/`.

---

## Keybindings

| Key | Action |
|-----|--------|
| Enter | Confirm / start crawl / crawl another |
| Esc | Cancel active crawl / stop server |
| Ctrl+S | Serve downloaded site |
| Ctrl+G | Open config wizard |
| Ctrl+Q | Quit |

---

## Config

The config file is created interactively on first launch (wizard steps through each setting).
You can re-open the wizard any time with `--setup` or `Ctrl+G`.

**Location:**
- macOS / Linux: `~/.config/go-scraper/config.toml`
- Windows: `%AppData%\go-scraper\config.toml`

```toml
output_dir        = "~/Downloads/go-scraper"
download_media    = true
max_media_size_mb = 100   # 0 = no cap
domain_depth      = 0     # 0 = starting domain only; 1 = one hop to external domains, etc.
max_depth         = 0     # 0 = unlimited page depth
serve_port        = 8080  # 0 = disabled
```

### Config options explained

| Option | Default | Notes |
|--------|---------|-------|
| `output_dir` | `~/Downloads/go-scraper` | Where files are saved. `~/` is expanded automatically. |
| `download_media` | `true` | When false, only HTML pages are saved (no images, CSS, JS, fonts). |
| `max_media_size_mb` | `100` | Skip media files larger than this. `0` disables the cap. |
| `domain_depth` | `0` | `0` = crawl only the starting domain. `1` = follow one hop to external domains, and so on. |
| `max_depth` | `0` | `0` = unlimited. `3` = at most 3 link hops from the start URL. |
| `serve_port` | `0` | Port for the built-in HTTP server (e.g. `8080`). `0` disables serving. |

---

## File naming

Files are saved under `output_dir`, mirroring the URL path structure. The extension is always taken from the `Content-Type` response header, so server-side scripts and clean URLs are saved correctly regardless of what the URL itself looks like.

| URL | Content-Type | Saved as |
|-----|-------------|----------|
| `example.com/` | `text/html` | `example.com/index.html` |
| `example.com/about` | `text/html` | `example.com/about.html` |
| `example.com/about/` | `text/html` | `example.com/about/index.html` |
| `example.com/index.php` | `text/html` | `example.com/index.html` |
| `example.com/style.css` | `text/css` | `example.com/style.css` |
| `example.com/img/logo.png` | `image/png` | `example.com/img/logo.png` |
| `example.com/api/data` | `application/json` | `example.com/api/data.json` |
| `example.com/api/data.php` | `application/json` | `example.com/api/data.json` |

Key decisions:
- Trailing-slash URLs (including root `/`) are saved as `index.html` inside the directory, matching how real web servers work.
- The saved extension always reflects what the server actually serves, not what the URL suggests. A `.php` URL served as `text/html` becomes `.html`.
- For unknown content-types (images, video, fonts), the original URL extension is kept as-is.
- Query strings and fragments are stripped (they do not map to files on disk).

---

## How it works (brief)

1. Add the start URL to a queue.
2. Pop a URL, skip if already visited, otherwise download it.
3. If the response is HTML: extract all `href`, `src`, `srcset` links and add qualifying ones to the queue.
4. Apply `domain_depth` and `max_depth` limits before enqueuing each link.
5. After the queue is empty, rewrite all saved HTML files in one pass — absolute URLs become relative local paths so the site works in a browser without a server.
6. Walk the output directory, build a file tree, sum total bytes, display on the done screen.

---

## Development

```sh
make build     # compile to ./go-scraper
make install   # build and install to /usr/local/bin (uses sudo if needed)
make run       # go run .
make test      # go test ./...
make lint      # go vet ./...
make clean     # remove binary
```

---

## Tech stack

| Concern | Library |
|---------|---------|
| TUI framework | `github.com/charmbracelet/bubbletea` |
| Terminal styles | `github.com/charmbracelet/lipgloss` |
| HTML parsing | `golang.org/x/net/html` |
| Config format | `github.com/BurntSushi/toml` |
| HTTP client / server | `net/http` (stdlib) |

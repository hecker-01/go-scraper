# go-scraper

A terminal UI tool written in Go that downloads a full website to a local folder.
It crawls recursively, rewrites links to local paths, and shows a live progress view while it works.

## Usage

```
go-scraper                             # open TUI, enter a URL interactively
go-scraper --url https://example.com  # open TUI and start crawling right away
go-scraper --setup                    # open the config wizard
```

## Features

- Recursive crawl with deduplication (no loops)
- Downloads HTML, media, CSS, JS and fonts
- Rewrites links so the site works offline in a browser
- Live TUI progress with spinner, per-file lines and error log
- File tree + total size summary when done
- TOML config stored in your OS config dir

## Config

Config is stored at:

- macOS/Linux: `~/.config/go-scraper/config.toml`
- Windows: `%AppData%\go-scraper\config.toml`

Run `go-scraper --setup` or press `Ctrl+S` inside the app to edit it.

```toml
output_dir        = "~/Downloads/go-scraper"
download_media    = true
max_media_size_mb = 100   # 0 = no cap
domain_depth      = 0     # 0 = starting domain only, 1 = one hop to external domains, etc.
max_depth         = 0     # 0 = unlimited page depth
```

## Install

```
git clone https://github.com/hecker-01/go-scraper
cd go-scraper
make build
```

Then move the `go-scraper` binary somewhere on your `$PATH`.

## Requirements

- Go 1.22 or newer

## Keybindings

| Key | Action |
|-----|--------|
| Enter | Confirm / start crawl |
| Esc | Cancel crawl |
| Ctrl+S | Open config wizard |
| Ctrl+Q | Quit |

package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

const version = "1.0.0"

func printHelp() {
	fmt.Printf(`go-scraper v%s

A terminal UI tool that downloads a full website to a local folder.
Crawls recursively, rewrites links for offline browsing, and shows a
live progress view while it works.

Usage:
  go-scraper [flags]

Flags:
  --url <URL>    Pre-fill URL and start crawling immediately
  --setup        Open the config wizard
  --serve        Serve the output directory over HTTP (requires serve_port to be configured)
  --version      Print version and exit
  --help         Show this help message

Keybindings (in TUI):
  Enter          Confirm / start crawl / crawl another
  Esc            Cancel active crawl / stop server
  Ctrl+S         Serve downloaded site
  Ctrl+G         Open config wizard
  Ctrl+Q         Quit

Config file:
  macOS / Linux  ~/.config/go-scraper/config.toml
  Windows        %%AppData%%\go-scraper\config.toml

`, version)
}

func main() {
	url   := flag.String("url", "", "URL to scrape (optional - can also be entered in the TUI)")
	setup := flag.Bool("setup", false, "Open the config wizard")
	serve := flag.Bool("serve", false, "Serve the output directory over HTTP")
	ver   := flag.Bool("version", false, "Print version and exit")
	help  := flag.Bool("help", false, "Show this help message")

	flag.Usage = printHelp
	flag.Parse()

	switch {
	case *help:
		printHelp()
	case *ver:
		fmt.Println("go-scraper v" + version)
	case *serve:
		cfg, existed, err := LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		if !existed {
			fmt.Fprintln(os.Stderr, "No config found. Run go-scraper --setup to configure first.")
			os.Exit(1)
		}
		if cfg.ServePort == 0 {
			fmt.Fprintln(os.Stderr, "Serve port not configured. Run go-scraper --setup to set one.")
			os.Exit(1)
		}
		if err := serveBlocking(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	default:
		m := initialModel(*url, *setup)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

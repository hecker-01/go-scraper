package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

const version = "1.0.0"

func main() {
	url     := flag.String("url", "", "URL to scrape (optional - can also be entered in the TUI)")
	setup   := flag.Bool("setup", false, "Open the config wizard")
	ver     := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *ver {
		fmt.Println("go-scraper v" + version)
		return
	}

	m := initialModel(*url, *setup)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

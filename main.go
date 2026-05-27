package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	url := flag.String("url", "", "URL to scrape (optional - can also be entered in the TUI)")
	setup := flag.Bool("setup", false, "Open the config wizard")
	flag.Parse()

	m := initialModel(*url, *setup)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

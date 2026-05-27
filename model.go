package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateConfig   state = iota // first-boot wizard, --setup flag, or Ctrl+S
	stateInput                 // URL input screen
	stateCrawling              // live crawl progress
	stateDone                  // summary + file tree
)

const numConfigSteps = 5

type model struct {
	state  state
	width  int
	height int

	// config wizard
	config          Config
	configStep      int
	configInput     string
	firstBoot       bool
	configSavedPath string

	// input screen
	input      string
	errMessage string

	// crawling
	spinner     spinner.Model
	outputCh    <-chan tea.Msg
	completed   int
	totalBytes  int64
	lastFile    string
	errorLog    []logLineMsg
	recentLog   []string
	cancelCrawl func()
	cancelling  bool

	// done
	treeOutput string

	// overlays
	quitConfirm bool
}

func initialModel(url string, setup bool) model {
	cfg, existed, _ := LoadConfig()
	if !existed {
		cfg = DefaultConfig()
	}

	s := stateInput
	if !existed || setup {
		s = stateConfig
	}

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	m := model{
		state:     s,
		config:    cfg,
		firstBoot: !existed,
		spinner:   sp,
	}

	// --url: pre-fill input; if config already exists jump straight to crawling
	if url != "" {
		m.input = url
		if !setup && existed {
			m.state = stateCrawling
		}
	}

	return m
}

func (m model) Init() tea.Cmd {
	if m.state == stateCrawling {
		return tea.Batch(startCrawl(m.input, m.config), m.spinner.Tick)
	}
	return nil
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		// Quit confirm overlay intercepts all keys.
		if m.quitConfirm {
			switch msg.Type {
			case tea.KeyEnter:
				return m, tea.Quit
			case tea.KeyEsc, tea.KeyCtrlC:
				m.quitConfirm = false
			}
			return m, nil
		}

		switch m.state {

		case stateConfig:
			switch msg.Type {
			case tea.KeyCtrlQ:
				m.quitConfirm = true
			case tea.KeyEsc, tea.KeyCtrlC:
				// Can only escape the wizard if it was opened manually (not first-boot).
				if !m.firstBoot {
					m.state = stateInput
					m.configStep = 0
					m.configInput = ""
				}
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.configInput) > 0 {
					m.configInput = m.configInput[:len(m.configInput)-1]
				}
			case tea.KeyEnter:
				m.applyConfigStep()
				m.configStep++
				m.configInput = ""
				if m.configStep >= numConfigSteps {
					path, _ := m.config.FilePath()
					_ = m.config.Save()
					m.configStep = 0
					m.firstBoot = false
					m.configSavedPath = path
					m.state = stateInput
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.configInput += string(msg.Runes)
				}
			}

		case stateInput:
			switch msg.Type {
			case tea.KeyCtrlQ, tea.KeyCtrlC, tea.KeyEsc:
				m.quitConfirm = true
			case tea.KeyCtrlS:
				m.state = stateConfig
				m.configStep = 0
				m.configInput = ""
				m.configSavedPath = ""
			case tea.KeyEnter:
				url := strings.TrimSpace(m.input)
				if !isValidURL(url) {
					m.errMessage = "Please enter a valid http:// or https:// URL."
					return m, nil
				}
				m.errMessage = ""
				m.configSavedPath = ""
				m.state = stateCrawling
				m.recentLog = nil
				m.errorLog = nil
				m.completed = 0
				m.totalBytes = 0
				m.lastFile = ""
				m.cancelling = false
				return m, tea.Batch(startCrawl(url, m.config), m.spinner.Tick)
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.input) > 0 {
					m.input = m.input[:len(m.input)-1]
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.input += string(msg.Runes)
				}
			}

		case stateCrawling:
			switch msg.Type {
			case tea.KeyEsc:
				if m.cancelCrawl != nil {
					m.cancelCrawl()
				}
				m.cancelling = true
			case tea.KeyCtrlQ, tea.KeyCtrlC:
				m.quitConfirm = true
			}

		case stateDone:
			switch msg.Type {
			case tea.KeyEnter:
				m.state = stateInput
				m.input = ""
				m.errMessage = ""
				m.recentLog = nil
				m.errorLog = nil
				m.treeOutput = ""
			case tea.KeyCtrlS:
				m.state = stateConfig
				m.configStep = 0
				m.configInput = ""
				m.configSavedPath = ""
			case tea.KeyCtrlQ, tea.KeyCtrlC, tea.KeyEsc:
				m.quitConfirm = true
			}
		}

	case spinner.TickMsg:
		if m.state == stateCrawling {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case crawlStartedMsg:
		m.outputCh = msg.ch
		m.cancelCrawl = msg.cancel
		// Edge case: user pressed Esc before the goroutine started.
		// Cancel immediately so it exits at the first ctx.Done() check.
		if m.cancelling {
			msg.cancel()
		}
		return m, waitForOutput(m.outputCh)

	case logLineMsg:
		if msg.level >= slog.LevelWarn {
			m.errorLog = append(m.errorLog, msg)
		} else {
			m.recentLog = append(m.recentLog, msg.line)
			if len(m.recentLog) > 3 {
				m.recentLog = m.recentLog[len(m.recentLog)-3:]
			}
		}
		return m, waitForOutput(m.outputCh)

	case fileDoneMsg:
		m.completed++
		m.totalBytes += msg.size
		m.lastFile = filepath.Base(msg.path)
		return m, waitForOutput(m.outputCh)

	case crawlDoneMsg:
		m.outputCh = nil
		m.cancelCrawl = nil
		m.treeOutput = msg.treeOutput
		m.totalBytes = msg.totalBytes
		wasCancelling := m.cancelling
		m.cancelling = false
		m.state = stateDone
		switch {
		case msg.err != nil:
			m.errMessage = "Error: " + msg.err.Error()
		case wasCancelling:
			m.errMessage = fmt.Sprintf("Cancelled. %d files saved (%s).", m.completed, formatBytes(m.totalBytes))
		default:
			m.errMessage = fmt.Sprintf("Done. %d files saved (%s).", m.completed, formatBytes(m.totalBytes))
		}
	}

	return m, nil
}

// ─── Config wizard helpers ────────────────────────────────────────────────────

// configFieldInfo returns the label, current value and optional hint for the
// current wizard step.
func (m model) configFieldInfo() (label, current, fieldHint string) {
	switch m.configStep {
	case 0:
		return "Output directory", m.config.OutputDir, ""
	case 1:
		return "Download media (images, CSS, JS, fonts)", boolToYesNo(m.config.DownloadMedia), "yes / no"
	case 2:
		return "Max media file size in MB", strconv.Itoa(m.config.MaxMediaSizeMB), "0 = no cap"
	case 3:
		return "Domain depth", strconv.Itoa(m.config.DomainDepth), "0 = starting domain only, 1 = one hop to external domains, etc."
	case 4:
		return "Max crawl depth", strconv.Itoa(m.config.MaxDepth), "0 = unlimited"
	}
	return "", "", ""
}

// applyConfigStep writes configInput into the correct Config field for the
// current step. Empty input keeps the existing value.
func (m *model) applyConfigStep() {
	v := m.configInput
	switch m.configStep {
	case 0:
		if v != "" {
			m.config.OutputDir = v
		}
	case 1:
		if v != "" {
			m.config.DownloadMedia = parseBool(v, m.config.DownloadMedia)
		}
	case 2:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.config.MaxMediaSizeMB = n
		}
	case 3:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.config.DomainDepth = n
		}
	case 4:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.config.MaxDepth = n
		}
	}
}

func (m model) viewConfig(b *strings.Builder, contentWidth int) {
	if m.firstBoot {
		b.WriteString(styleHighlight.Render("Welcome! Set up your config to get started."))
	} else {
		b.WriteString(stylePrompt.Render(fmt.Sprintf("Configuration (%d of %d)", m.configStep+1, numConfigSteps)))
	}
	b.WriteString("\n\n")

	label, current, fieldHint := m.configFieldInfo()
	b.WriteString(stylePrompt.Render(label))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("Current: ") + styleInput.Render(current))
	b.WriteString("\n")
	if fieldHint != "" {
		b.WriteString(renderWrap(styleDim, fieldHint, contentWidth))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render("New value (empty = keep current): "))
	b.WriteString(styleInput.Render(m.configInput))
	b.WriteString(styleInput.Render("█"))
	b.WriteString("\n\n")
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	width := m.width
	height := m.height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	const minW, minH = 40, 16
	if width < minW || height < minH {
		msg := fmt.Sprintf(" Terminal too small!\n Resize to at least %d x %d ", minW, minH)
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("1")).
			Foreground(lipgloss.Color("1")).
			Bold(true).
			Padding(1, 2).
			Render(msg)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
	}

	if m.quitConfirm {
		return m.viewQuitConfirm(width, height)
	}

	contentWidth := width - 6
	if contentWidth < 20 {
		contentWidth = 20
	}
	maxErrors := height - 15
	if maxErrors < 1 {
		maxErrors = 1
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(1, 2).
		Width(contentWidth + 4)

	// innerHeight = screen height minus border (2 rows) and padding (2 rows).
	innerHeight := height - 4

	var top strings.Builder
	top.WriteString(styleTitle.Render("go-scraper"))
	top.WriteString("\n")

	switch m.state {
	case stateConfig:
		top.WriteString("\n")
		m.viewConfig(&top, contentWidth)

	case stateInput:
		top.WriteString(renderWrap(styleDim, "Enter a URL to scrape.", contentWidth))
		top.WriteString("\n\n")
		if m.configSavedPath != "" {
			top.WriteString(styleSuccess.Render("Config saved to "))
			top.WriteString(hyperlinkFile(m.configSavedPath))
			top.WriteString("\n\n")
		}
		top.WriteString(stylePrompt.Render("-> "))
		top.WriteString(styleInput.Render(m.input))
		top.WriteString(styleInput.Render("█"))
		top.WriteString("\n")
		if isValidURL(m.input) {
			dest := expandHome(m.config.OutputDir)
			top.WriteString(styleDim.Render("   Will save to: "))
			top.WriteString(hyperlinkFile(dest))
			top.WriteString("\n")
		}
		if m.errMessage != "" {
			top.WriteString("\n")
			top.WriteString(renderWrap(styleError, m.errMessage, contentWidth))
			top.WriteString("\n")
		}

	case stateCrawling:
		top.WriteString(renderWrap(styleDim, "Enter a URL to scrape.", contentWidth))
		top.WriteString("\n\n")
		if m.cancelling {
			top.WriteString(m.spinner.View())
			top.WriteString(" ")
			top.WriteString(renderWrap(styleDim, "Cancelling...", contentWidth-2))
			top.WriteString("\n")
		} else {
			top.WriteString(m.spinner.View())
			top.WriteString(" ")
			parts := fmt.Sprintf("%d files", m.completed)
			if m.totalBytes > 0 {
				parts += " / " + formatBytes(m.totalBytes)
			}
			if m.lastFile != "" {
				parts += " / " + truncate(m.lastFile, 30)
			}
			top.WriteString(renderWrap(stylePrompt, parts, contentWidth-2))
			top.WriteString("\n")
		}
		writeErrors(&top, m.errorLog, contentWidth, maxErrors)
		writeLog(&top, m.recentLog, contentWidth)

	case stateDone:
		top.WriteString(renderWrap(styleDim, "Enter a URL to scrape.", contentWidth))
		top.WriteString("\n\n")
		switch {
		case strings.HasPrefix(m.errMessage, "Error"):
			top.WriteString(renderWrap(styleError, m.errMessage, contentWidth))
		case strings.HasPrefix(m.errMessage, "Cancelled"):
			top.WriteString(renderWrap(styleDim, m.errMessage, contentWidth))
		default:
			top.WriteString(renderWrap(styleSuccess, m.errMessage, contentWidth))
		}
		if m.treeOutput != "" {
			top.WriteString("\n\n")
			top.WriteString(styleDim.Render(m.treeOutput))
		}
		writeErrors(&top, m.errorLog, contentWidth, maxErrors)
	}

	// Pin the hint bar to the bottom by padding with blank lines.
	topStr := top.String()
	hintStr := m.hintBar(contentWidth)
	topLines := strings.Count(topStr, "\n") + 1
	blankCount := innerHeight - topLines
	if blankCount < 0 {
		blankCount = 0
	}

	var b strings.Builder
	b.WriteString(topStr)
	b.WriteString(strings.Repeat("\n", blankCount))
	if hintStr != "" {
		b.WriteString(hintStr)
	}

	return border.Render(b.String())
}

// hintBar renders the keybinding footer for the current state.
func (m model) hintBar(_ int) string {
	switch m.state {
	case stateConfig:
		if m.firstBoot {
			return hint("Ctrl+Q", "Quit")
		}
		return hint("Esc", "Cancel") + "   " + hint("Ctrl+Q", "Quit")
	case stateInput:
		return hint("Enter", "Crawl") + "   " + hint("Ctrl+S", "Configure") + "   " + hint("Ctrl+Q", "Quit")
	case stateCrawling:
		return hint("Esc", "Cancel") + "   " + hint("Ctrl+Q", "Quit")
	case stateDone:
		return hint("Enter", "Crawl another") + "   " + hint("Ctrl+S", "Configure") + "   " + hint("Ctrl+Q", "Quit")
	}
	return ""
}

// viewQuitConfirm renders the quit confirmation popup centered on screen.
func (m model) viewQuitConfirm(width, height int) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Quit go-scraper?"))
	b.WriteString("\n\n")
	if m.state == stateCrawling {
		b.WriteString(styleDim.Render("The active crawl will be cancelled."))
		b.WriteString("\n\n")
	}
	b.WriteString(
		styleHighlight.Render("Enter") + " " + styleError.Render("Quit") +
			"   " +
			styleHighlight.Render("Esc") + " " + styleDim.Render("Cancel"),
	)

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("1")).
		Padding(1, 3).
		Render(b.String())

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}

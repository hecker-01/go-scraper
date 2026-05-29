package main

import (
	"fmt"
	"log/slog"
	"math/rand"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// miniLines is the compact ASCII art used as the header on every non-intro screen.
var miniLines = []string{
	`┏━╸┏━┓   ┏━┓┏━╸┏━┓┏━┓┏━┓┏━╸┏━┓`,
	`┃╺┓┃ ┃╺━╸┗━┓┃  ┣┳┛┣━┫┣━┛┣╸ ┣┳┛`,
	`┗━┛┗━┛   ┗━┛┗━╸╹┗╸╹ ╹╹  ┗━╸╹┗╸`,
}

// introLines is the ASCII art logo shown during the startup animation.
var introLines = []string{
	`   ____         ____                                 `,
	`  / ___| ___   / ___|  ___ _ __ __ _ _ __   ___ _ __ `,
	" | |  _ / _ \\  \\___ \\ / __| '__/ _" + "`" + " | '_ \\ / _ \\ '__|",
	` | |_| | (_) |  ___) | (__| | | (_| | |_) |  __/ |   `,
	`  \____|\___/  |____/ \___|_|  \__,_| .__/ \___|_|   `,
	`                                    |_|`,
}

// Characters sampled for the scramble zone — art-like symbols only.
var introScrChars = []rune(`_/\|-.*+=#@!?~,;:<>[]^()'`)

const (
	introCharsPerTick = 2  // settled chars revealed per tick
	introScrambleLen  = 10 // scramble zone width (chars ahead of settled pos)
	introTickMs       = 8  // ms between ticks
)

func newIntroScramble() []rune {
	s := make([]rune, introScrambleLen)
	for i := range s {
		s[i] = introScrChars[rand.Intn(len(introScrChars))]
	}
	return s
}

type introTickMsg struct{}
type introTaglineMsg struct{}
type introDoneMsg struct{}

func introTick() tea.Cmd {
	return tea.Tick(time.Duration(introTickMs)*time.Millisecond, func(time.Time) tea.Msg { return introTickMsg{} })
}

type state int

const (
	stateIntro    state = iota // startup animation
	stateConfig                // first-boot wizard, --setup flag, or Ctrl+S
	stateInput                 // URL input screen
	stateCrawling              // live crawl progress
	stateDone                  // summary + file tree
	stateServing               // HTTP file server running
)

const numConfigSteps = 6

type model struct {
	state  state
	width  int
	height int

	// intro animation
	introPos         int    // rune index of the first un-settled character
	introScramble    []rune // random chars displayed ahead of introPos
	introShowTagline bool   // whether the "By heckr.dev · vX" line is visible
	introNextState   state  // state to enter after the animation

	// config wizard
	config          Config
	configStep      int
	configInput     string
	configCursor    int  // rune index of the insertion point in configInput
	configBoolVal   bool // selection state for boolean steps (step 1)
	firstBoot       bool
	configSavedPath string

	// input screen
	input       string
	inputCursor int // rune index of the insertion point in input
	errMessage  string

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
	treeScroll int // index of the first visible tree line

	// serving
	serveURL         string
	serveNetworkURL  string
	stopServer       func()
	servingPrevState state // screen to return to when Esc is pressed from stateServing

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
		state:          stateIntro,
		introNextState: s,
		introScramble:  newIntroScramble(),
		config:         cfg,
		configBoolVal:  cfg.DownloadMedia,
		firstBoot:      !existed,
		spinner:        sp,
	}

	// Skip the intro whenever any CLI flag is supplied.
	if url != "" || setup {
		m.state = s
		if url != "" {
			m.input = url
			m.inputCursor = len([]rune(url))
			if !setup && existed {
				m.state = stateCrawling
			}
		}
	}

	return m
}

func (m model) Init() tea.Cmd {
	switch m.state {
	case stateIntro:
		return introTick()
	case stateCrawling:
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
			case tea.KeyEnter, tea.KeyCtrlC:
				if m.stopServer != nil {
					m.stopServer()
				}
				return m, tea.Quit
			case tea.KeyEsc:
				m.quitConfirm = false
			}
			return m, nil
		}

		switch m.state {

		case stateIntro:
			switch msg.Type {
			case tea.KeyCtrlQ, tea.KeyCtrlC:
				m.quitConfirm = true
			default:
				// Any other key skips the animation immediately.
				m.state = m.introNextState
				if m.state == stateCrawling {
					return m, tea.Batch(startCrawl(m.input, m.config), m.spinner.Tick)
				}
			}

		case stateConfig:
			switch msg.Type {
			case tea.KeyCtrlQ, tea.KeyCtrlC:
				m.quitConfirm = true
			case tea.KeyEsc:
				// Can only escape the wizard if it was opened manually (not first-boot).
				if !m.firstBoot {
					m.state = stateInput
					m.configStep = 0
					m.configInput = ""
					m.configCursor = 0
				}
			case tea.KeyLeft:
				if m.configStep == 1 {
					m.configBoolVal = !m.configBoolVal
				} else if m.configCursor > 0 {
					m.configCursor--
				}
			case tea.KeyRight:
				if m.configStep == 1 {
					m.configBoolVal = !m.configBoolVal
				} else if m.configCursor < len([]rune(m.configInput)) {
					m.configCursor++
				}
			case tea.KeyUp, tea.KeyDown:
				if m.configStep == 1 {
					m.configBoolVal = !m.configBoolVal
				}
			case tea.KeyHome, tea.KeyCtrlA:
				m.configCursor = 0
			case tea.KeyEnd, tea.KeyCtrlE:
				m.configCursor = len([]rune(m.configInput))
			case tea.KeyBackspace:
				runes := []rune(m.configInput)
				if m.configCursor > 0 {
					m.configInput = string(append(runes[:m.configCursor-1:m.configCursor-1], runes[m.configCursor:]...))
					m.configCursor--
				}
			case tea.KeyDelete:
				runes := []rune(m.configInput)
				if m.configCursor < len(runes) {
					m.configInput = string(append(runes[:m.configCursor:m.configCursor], runes[m.configCursor+1:]...))
				}
			case tea.KeyEnter:
				m.applyConfigStep()
				m.configStep++
				m.configInput = ""
				m.configCursor = 0
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
					runes := []rune(m.configInput)
					for _, r := range msg.Runes {
						// Numeric steps: only accept digit characters.
						if m.configStep >= 2 && (r < '0' || r > '9') {
							continue
						}
						runes = append(runes[:m.configCursor:m.configCursor], append([]rune{r}, runes[m.configCursor:]...)...)
						m.configCursor++
					}
					m.configInput = string(runes)
				}
			}

		case stateInput:
			switch msg.Type {
			case tea.KeyCtrlQ, tea.KeyCtrlC, tea.KeyEsc:
				m.quitConfirm = true
			case tea.KeyCtrlG:
				m.state = stateConfig
				m.configStep = 0
				m.configInput = ""
				m.configCursor = 0
				m.configBoolVal = m.config.DownloadMedia
				m.configSavedPath = ""
			case tea.KeyCtrlS:
				if m.config.ServePort == 0 {
					m.errMessage = "Serve port not configured. Press Ctrl+G to configure."
				} else {
					m.servingPrevState = stateInput
					return m, startServerCmd(m.config.OutputDir, m.config.ServePort)
				}
			case tea.KeyLeft:
				if m.inputCursor > 0 {
					m.inputCursor--
				}
			case tea.KeyRight:
				if m.inputCursor < len([]rune(m.input)) {
					m.inputCursor++
				}
			case tea.KeyHome, tea.KeyCtrlA:
				m.inputCursor = 0
			case tea.KeyEnd, tea.KeyCtrlE:
				m.inputCursor = len([]rune(m.input))
			case tea.KeyBackspace:
				runes := []rune(m.input)
				if m.inputCursor > 0 {
					m.input = string(append(runes[:m.inputCursor-1:m.inputCursor-1], runes[m.inputCursor:]...))
					m.inputCursor--
				}
			case tea.KeyDelete:
				runes := []rune(m.input)
				if m.inputCursor < len(runes) {
					m.input = string(append(runes[:m.inputCursor:m.inputCursor], runes[m.inputCursor+1:]...))
				}
			case tea.KeyEnter:
				url := addScheme(m.input)
				if !isValidURL(url) {
					m.errMessage = "Please enter a valid URL or domain name (e.g. heckr.dev)."
					return m, nil
				}
				// Write the normalised form back so the display shows https://...
				m.input = url
				m.inputCursor = len([]rune(url))
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
			default:
				if msg.Type == tea.KeyRunes {
					runes := []rune(m.input)
					for _, r := range msg.Runes {
						runes = append(runes[:m.inputCursor:m.inputCursor], append([]rune{r}, runes[m.inputCursor:]...)...)
						m.inputCursor++
					}
					m.input = string(runes)
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
			case tea.KeyUp:
				if m.treeScroll > 0 {
					m.treeScroll--
				}
			case tea.KeyDown:
				m.treeScroll++
			case tea.KeyEnter:
				m.state = stateInput
				m.input = ""
				m.inputCursor = 0
				m.errMessage = ""
				m.recentLog = nil
				m.errorLog = nil
				m.treeOutput = ""
				m.treeScroll = 0
			case tea.KeyCtrlG:
				m.state = stateConfig
				m.configStep = 0
				m.configInput = ""
				m.configCursor = 0
				m.configBoolVal = m.config.DownloadMedia
				m.configSavedPath = ""
				m.treeScroll = 0
			case tea.KeyCtrlS:
				if m.config.ServePort == 0 {
					m.errMessage = "Serve port not configured. Press Ctrl+G to configure."
				} else {
					m.servingPrevState = stateDone
					return m, startServerCmd(m.config.OutputDir, m.config.ServePort)
				}
			case tea.KeyEsc:
				m.state = stateInput
				m.input = ""
				m.inputCursor = 0
				m.errMessage = ""
				m.recentLog = nil
				m.errorLog = nil
				m.treeOutput = ""
				m.treeScroll = 0
			case tea.KeyCtrlQ, tea.KeyCtrlC:
				m.quitConfirm = true
			default:
				if msg.Type == tea.KeyRunes && string(msg.Runes) == "s" {
					if m.config.ServePort == 0 {
						m.errMessage = "Serve port not configured. Press Ctrl+G to configure."
					} else {
						m.servingPrevState = stateDone
						return m, startServerCmd(m.config.OutputDir, m.config.ServePort)
					}
				}
			}

		case stateServing:
			switch msg.Type {
			case tea.KeyEsc:
				if m.stopServer != nil {
					m.stopServer()
					m.stopServer = nil
				}
				m.serveURL = ""
				m.serveNetworkURL = ""
				m.state = m.servingPrevState
			case tea.KeyCtrlQ, tea.KeyCtrlC:
				m.quitConfirm = true
			}
		}

	case tea.MouseMsg:
		if m.state == stateDone {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if m.treeScroll > 0 {
					m.treeScroll--
				}
			case tea.MouseButtonWheelDown:
				m.treeScroll++
			}
		}

	case introTickMsg:
		if m.state != stateIntro {
			return m, nil
		}
		artLen := len([]rune(strings.Join(introLines, "\n")))
		m.introPos += introCharsPerTick
		if m.introPos > artLen {
			m.introPos = artLen
		}
		m.introScramble = newIntroScramble()
		if m.introPos >= artLen {
			m.introScramble = nil
			// Short pause, then reveal the tagline.
			return m, tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg { return introTaglineMsg{} })
		}
		return m, introTick()

	case introTaglineMsg:
		if m.state != stateIntro {
			return m, nil
		}
		m.introShowTagline = true
		// Hold the complete intro for a moment before transitioning.
		return m, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg { return introDoneMsg{} })

	case introDoneMsg:
		if m.state != stateIntro {
			return m, nil
		}
		m.state = m.introNextState
		if m.state == stateCrawling {
			return m, tea.Batch(startCrawl(m.input, m.config), m.spinner.Tick)
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
		case m.completed == 0:
			m.errMessage = "Failed. No files were saved."
		default:
			m.errMessage = fmt.Sprintf("Done. %d files saved (%s).", m.completed, formatBytes(m.totalBytes))
		}

	case serverStartedMsg:
		m.serveURL = msg.url
		m.serveNetworkURL = msg.networkURL
		m.stopServer = msg.stop
		m.state = stateServing

	case serverErrorMsg:
		m.errMessage = "Server error: " + msg.err.Error()
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
		return "Download media (images, CSS, JS, fonts)", boolToStr(m.config.DownloadMedia), ""
	case 2:
		return "Max media file size in MB", strconv.Itoa(m.config.MaxMediaSizeMB), "0 = no cap"
	case 3:
		return "Domain depth", strconv.Itoa(m.config.DomainDepth), "0 = starting domain only, 1 = one hop to external domains, etc."
	case 4:
		return "Max crawl depth", strconv.Itoa(m.config.MaxDepth), "0 = unlimited"
	case 5:
		return "Serve port", strconv.Itoa(m.config.ServePort), "port for the built-in HTTP server (e.g. 8080), 0 = disabled"
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
		m.config.DownloadMedia = m.configBoolVal
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
	case 5:
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			m.config.ServePort = n
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
	if m.configStep == 1 {
		// Boolean selector — navigate with ← / →.
		trueStyle := styleDim
		falseStyle := styleDim
		if m.configBoolVal {
			trueStyle = stylePrompt
		} else {
			falseStyle = stylePrompt
		}
		b.WriteString(trueStyle.Render("true"))
		b.WriteString(styleDim.Render("  /  "))
		b.WriteString(falseStyle.Render("false"))
		b.WriteString("\n\n")
	} else {
		runes := []rune(m.configInput)
		before := string(runes[:m.configCursor])
		cursorCh, after := " ", ""
		if m.configCursor < len(runes) {
			cursorCh = string(runes[m.configCursor])
			after = string(runes[m.configCursor+1:])
		}
		b.WriteString(styleDim.Render("New value (empty = keep current): "))
		b.WriteString(styleInput.Render(before))
		b.WriteString(styleCursor.Render(cursorCh))
		b.WriteString(styleInput.Render(after))
		b.WriteString("\n\n")
	}
}

// viewDone renders the stateDone screen: the crawled URL, the result line
// (done / cancelled / error), a clickable folder link, and the file tree
// (capped to maxTreeLines rows to stay within the visible box).
func (m model) viewDone(b *strings.Builder, contentWidth, maxTreeLines int) {
	// URL that was just crawled.
	b.WriteString(styleInput.Render(truncate(m.input, contentWidth)))
	b.WriteString("\n\n")

	// Status line - colour changes based on outcome.
	switch {
	case strings.HasPrefix(m.errMessage, "Error"), strings.HasPrefix(m.errMessage, "Failed"):
		b.WriteString(renderWrap(styleError, m.errMessage, contentWidth))
	case strings.HasPrefix(m.errMessage, "Cancelled"):
		b.WriteString(renderWrap(styleDim, m.errMessage, contentWidth))
	default:
		b.WriteString(renderWrap(styleSuccess, m.errMessage, contentWidth))
	}

	// Clickable link to the output folder — only when files were actually saved.
	if m.completed > 0 {
		outDir := expandHome(m.config.OutputDir)
		b.WriteString("\n")
		b.WriteString(styleDim.Render("Saved to: "))
		b.WriteString(hyperlinkFile(outDir))
		b.WriteString("\n")
	}

	// File tree — scrollable window into the full tree.
	if m.treeOutput != "" {
		allLines := strings.Split(m.treeOutput, "\n")
		total := len(allLines)

		// Clamp scroll to valid range.
		scroll := m.treeScroll
		if maxScroll := total - maxTreeLines; scroll > maxScroll {
			scroll = maxScroll
		}
		if scroll < 0 {
			scroll = 0
		}

		above := scroll
		below := total - scroll - maxTreeLines
		if below < 0 {
			below = 0
		}

		// Each active indicator consumes one row from the visible window.
		usable := maxTreeLines
		if above > 0 {
			usable--
		}
		if below > 0 {
			usable--
		}
		end := scroll + usable
		if end > total {
			end = total
		}

		b.WriteString("\n")
		if above > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("  ↑ %d above", above)))
			b.WriteString("\n")
		}
		b.WriteString(styleDim.Render(strings.Join(allLines[scroll:end], "\n")))
		if below > 0 {
			b.WriteString("\n")
			b.WriteString(styleDim.Render(fmt.Sprintf("  ↓ %d below", below)))
		}
		b.WriteString("\n")
	}
}

// viewServing renders the stateServing screen showing the active server URLs.
func (m model) viewServing(b *strings.Builder, contentWidth int) {
	b.WriteString(stylePrompt.Render("Serving scraped site locally"))
	b.WriteString("\n\n")
	b.WriteString(styleDim.Render("Local:   "))
	b.WriteString(hyperlinkHTTP(m.serveURL, styleSuccess.Render(m.serveURL)))
	b.WriteString("\n")
	if m.serveNetworkURL != "" {
		b.WriteString(styleDim.Render("Network: "))
		b.WriteString(hyperlinkHTTP(m.serveNetworkURL, styleSuccess.Render(m.serveNetworkURL)))
		b.WriteString("\n")
	}
	b.WriteString(styleDim.Render("Root:    "))
	b.WriteString(hyperlinkFile(expandHome(m.config.OutputDir)))
	b.WriteString("\n\n")
	b.WriteString(renderWrap(styleDim, "Open a URL in your browser to browse the downloaded site.", contentWidth))
	b.WriteString("\n")
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

	const minW, minH = 64, 20
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

	// innerHeight = screen height minus border (2 rows) and padding (2 rows).
	innerHeight := height - 4

	// Budget rows for variable-height sections (tree + errors).
	// Fixed overhead: mini header + 1 blank + URL + blank + status + blank + saved + blank-before-tree + blank-after-tree = headerH + 9.
	headerH := len(miniLines)
	avail := innerHeight - headerH - 9
	if avail < 4 {
		avail = 4
	}
	// Cap for how many error lines to show if errors exist.
	maxErrors := avail * 2 / 5
	if maxErrors < 1 {
		maxErrors = 1
	}
	// Only reserve lines for errors that will actually be rendered.
	actualErrLines := 0
	if n := len(m.errorLog); n > 0 {
		shown := n
		if shown > maxErrors {
			shown = maxErrors
		}
		actualErrLines = shown + 2 // +2 for the leading "\n\n" in writeErrors
		if n > maxErrors {
			actualErrLines++ // "...and N more" line
		}
	}
	maxTreeLines := avail - actualErrLines
	if maxTreeLines < 3 {
		maxTreeLines = 3
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(1, 2).
		Width(contentWidth + 4)

	var top strings.Builder
	if m.state != stateIntro {
		for _, line := range miniLines {
			top.WriteString(styleTitle.Render(line))
			top.WriteString("\n")
		}
	}

	switch m.state {
	case stateIntro:
		artRunes := []rune(strings.Join(introLines, "\n"))
		total := len(artRunes)
		pos := m.introPos
		if pos > total {
			pos = total
		}
		scramEnd := pos + introScrambleLen
		if scramEnd > total {
			scramEnd = total
		}

		// Centering
		artWidth := 0
		for _, l := range introLines {
			if w := len([]rune(strings.TrimRight(l, " "))); w > artWidth {
				artWidth = w
			}
		}
		margin := (contentWidth - artWidth) / 2
		if margin < 0 {
			margin = 0
		}
		pad := strings.Repeat(" ", margin)

		// Vertical centering
		topPad := (innerHeight - len(introLines)) / 2
		if topPad > 1 {
			top.WriteString(strings.Repeat("\n", topPad-1))
		}

		// Render character by character, left to right across each line.
		// • settled  (< pos):           styleTitle  (real char)
		// • scramble (pos..scramEnd-1): styleHighlight if non-space (random char),
		//                               space kept as-is so the art shape is legible
		// • not yet revealed:           hidden
		top.WriteString(pad)
		var seg strings.Builder // accumulates same-style chars before flushing
		flush := func(style lipgloss.Style) {
			if seg.Len() > 0 {
				top.WriteString(style.Render(seg.String()))
				seg.Reset()
			}
		}
		for i := 0; i < scramEnd; i++ {
			r := artRunes[i]
			if r == '\n' {
				flush(styleTitle)
				top.WriteString("\n")
				top.WriteString(pad)
				continue
			}
			if i < pos {
				seg.WriteRune(r)
			} else {
				// entering scramble zone — flush settled segment first
				flush(styleTitle)
				if r == ' ' {
					top.WriteString(" ")
				} else {
					si := i - pos
					var ch rune
					if si < len(m.introScramble) {
						ch = m.introScramble[si]
					} else {
						ch = r
					}
					top.WriteString(styleHighlight.Render(string(ch)))
				}
			}
		}
		flush(styleTitle)
		top.WriteString("\n")

		if m.introShowTagline {
			plain := "By heckr.dev  ·  v" + version
			tMargin := (contentWidth - len([]rune(plain))) / 2
			if tMargin < 0 {
				tMargin = 0
			}
			top.WriteString("\n")
			top.WriteString(strings.Repeat(" ", tMargin))
			top.WriteString(styleDim.Render("By "))
			top.WriteString(stylePrompt.Render("heckr.dev"))
			top.WriteString(styleDim.Render("  ·  v" + version))
			top.WriteString("\n")
		}

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
		inputRunes := []rune(m.input)
		inputBefore := string(inputRunes[:m.inputCursor])
		inputCursorCh, inputAfter := " ", ""
		if m.inputCursor < len(inputRunes) {
			inputCursorCh = string(inputRunes[m.inputCursor])
			inputAfter = string(inputRunes[m.inputCursor+1:])
		}
		top.WriteString(stylePrompt.Render("-> "))
		top.WriteString(styleInput.Render(inputBefore))
		top.WriteString(styleCursor.Render(inputCursorCh))
		top.WriteString(styleInput.Render(inputAfter))
		top.WriteString("\n")
		if isValidURL(addScheme(m.input)) {
			dest := expandHome(m.config.OutputDir)
			if u, err := url.Parse(addScheme(m.input)); err == nil && u.Hostname() != "" {
				dest = filepath.Join(dest, u.Hostname())
			}
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
		top.WriteString(styleDim.Render(truncate(m.input, contentWidth)))
		top.WriteString("\n")
		top.WriteString(styleDim.Render("Saving to: "))
		top.WriteString(hyperlinkFile(expandHome(m.config.OutputDir)))
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
		top.WriteString("\n")
		m.viewDone(&top, contentWidth, maxTreeLines)
		writeErrors(&top, m.errorLog, contentWidth, maxErrors)

	case stateServing:
		top.WriteString("\n")
		m.viewServing(&top, contentWidth)
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
	case stateIntro:
		return hint("Any key", "Skip")
	case stateConfig:
		var arrowHint string
		if m.configStep == 1 {
			arrowHint = hint("← →", "Toggle") + "   "
		}
		if m.firstBoot {
			return arrowHint + hint("Ctrl+Q", "Quit")
		}
		return arrowHint + hint("Esc", "Cancel") + "   " + hint("Ctrl+Q", "Quit")
	case stateInput:
		return hint("Enter", "Crawl") + "   " + hint("Ctrl+S", "Serve") + "   " + hint("Ctrl+G", "Configure") + "   " + hint("Ctrl+Q", "Quit")
	case stateCrawling:
		return hint("Esc", "Cancel") + "   " + hint("Ctrl+Q", "Quit")
	case stateDone:
		var serveHint string
		if m.config.ServePort > 0 {
			serveHint = hint("Ctrl+S", "Serve") + "   "
		}
		h := serveHint + hint("Enter", "Crawl another") + "   " + hint("Ctrl+G", "Configure") + "   " + hint("Ctrl+Q", "Quit")
		if m.treeOutput != "" {
			h = hint("↑↓", "Scroll") + "   " + h
		}
		return h
	case stateServing:
		return hint("Esc", "Stop serving") + "   " + hint("Ctrl+Q", "Quit")
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

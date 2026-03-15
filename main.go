package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	passStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	failStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	skipStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	sepStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	focusSep      = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	boldStyle     = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	logHighlight  = lipgloss.NewStyle().Background(lipgloss.Color("58"))
)

// ─── Key bindings ─────────────────────────────────────────────────────────────

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	PgUp     key.Binding
	PgDown   key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Filter   key.Binding
	Deselect key.Binding
	Tab      key.Binding
	Quit     key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Filter, k.Tab, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PgUp, k.PgDown, k.Top, k.Bottom},
		{k.Filter, k.Deselect, k.Tab, k.Quit},
	}
}

var defaultKeyMap = keyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PgUp:     key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "pg up")),
	PgDown:   key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "pg down")),
	Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
	Bottom:   key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
	Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Deselect: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "all/clear")),
	Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// ─── Test state ───────────────────────────────────────────────────────────────

type testStatus int

const (
	statusRunning testStatus = iota
	statusPass
	statusFail
	statusSkip
)

type testEntry struct {
	name   string
	status testStatus
}

// testItem is a row in the left panel.
// name == "" is the special "All tests" entry, always first.
type testItem struct {
	name   string
	status testStatus
}

// ─── Config ───────────────────────────────────────────────────────────────────

type config struct {
	outputDir  string
	keepLogs   bool
	clean      bool
	goTestArgs []string // extra args forwarded to go test (everything after --)
}

// ─── Tea messages ─────────────────────────────────────────────────────────────

type testEventMsg struct {
	action string
	test   string
	output string
}

type doneMsg struct {
	exitCode int
}

// ─── Model ────────────────────────────────────────────────────────────────────

const (
	focusTests = 0
	focusLog   = 1
)

type model struct {
	spinner     spinner.Model
	spinnerView string
	help        help.Model
	keys        keyMap

	// Right panel: log output.
	logVP   viewport.Model
	vpReady bool

	// Left panel: items + manual scroll.
	items            []testItem
	selectedIdx      int
	listScrollOffset int
	filterInput      textinput.Model
	filterActive     bool
	pinned           bool // auto-scroll to newest test as they arrive

	selectedTest string // which test's logs to show ("" = all)
	focus        int

	tests            map[string]*testEntry
	order            []string            // all test names seen, in arrival order
	testLogs         map[string][]string // per-test log lines
	logLines         []string            // all log lines (interleaved)
	logLineTests     []string            // parallel to logLines: owning test
	logHighlightLine int                 // line index to highlight in log VP (-1 = none)

	done     bool
	exitCode int
	runDir   string

	width, height int

	cfg    config
	cancel context.CancelFunc
}

func newModel(cfg config, runDir string, cancel context.CancelFunc) model {
	s := spinner.New(
		spinner.WithSpinner(spinner.MiniDot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))),
	)

	fi := textinput.New()
	fi.Placeholder = "filter tests…"
	fi.CharLimit = 60

	return model{
		spinner:          s,
		spinnerView:      s.View(),
		help:             help.New(),
		keys:             defaultKeyMap,
		items:            []testItem{},
		filterInput:      fi,
		tests:            make(map[string]*testEntry),
		testLogs:         make(map[string][]string),
		pinned:           true,
		logHighlightLine: -1,
		cfg:              cfg,
		runDir:           runDir,
		cancel:           cancel,
	}
}

func (m model) Init() tea.Cmd {
	return m.spinner.Tick
}

// ─── Layout helpers ───────────────────────────────────────────────────────────

func (m model) leftWidth() int {
	w := m.width * 35 / 100
	if w < 22 {
		w = 22
	}
	if w > 55 {
		w = 55
	}
	return w
}

func (m model) rightWidth() int {
	w := m.width - m.leftWidth() - 1
	if w < 10 {
		w = 10
	}
	return w
}

func (m model) panelHeight() int {
	h := m.height - 3 // 2 header rows + 1 footer row
	if h < 3 {
		h = 3
	}
	return h
}

// ─── List item management ─────────────────────────────────────────────────────

// rebuildListItems reconstructs the items slice: "All" first, then tests
// filtered by the current query. Preserves selectedIdx if item still present.
func (m *model) rebuildListItems() {
	query := strings.ToLower(m.filterInput.Value())
	items := make([]testItem, 0, len(m.order)+1)
	for _, name := range m.order {
		if query == "" || strings.Contains(strings.ToLower(name), query) {
			items = append(items, testItem{name: name, status: m.tests[name].status})
		}
	}
	// Preserve cursor on the same test name.
	prevName := ""
	if m.selectedIdx < len(m.items) {
		prevName = m.items[m.selectedIdx].name
	}
	m.items = items
	// Re-find cursor position.
	found := false
	for i, it := range m.items {
		if it.name == prevName {
			m.selectedIdx = i
			found = true
			break
		}
	}
	if !found {
		m.selectedIdx = 0
	}
	m.clampSelectedIdx()
	m.clampListScrollOffset()
}

// syncSelectedTest reads the currently highlighted item and updates selectedTest.
func (m *model) syncSelectedTest() {
	if m.selectedIdx < len(m.items) {
		m.selectedTest = m.items[m.selectedIdx].name
	} else {
		m.selectedTest = ""
	}
}

// selectItemByName moves the cursor to the item with the given name.
func (m *model) selectItemByName(name string) {
	for i, it := range m.items {
		if it.name == name {
			m.selectedIdx = i
			return
		}
	}
}

func (m *model) clampSelectedIdx() {
	if len(m.items) == 0 {
		m.selectedIdx = 0
		return
	}
	if m.selectedIdx >= len(m.items) {
		m.selectedIdx = len(m.items) - 1
	}
	if m.selectedIdx < 0 {
		m.selectedIdx = 0
	}
}

// scrollListToCursor adjusts listScrollOffset so the cursor stays visible.
func (m *model) scrollListToCursor() {
	ph := m.panelHeight()
	if m.selectedIdx < m.listScrollOffset {
		m.listScrollOffset = m.selectedIdx
	}
	if m.selectedIdx >= m.listScrollOffset+ph {
		m.listScrollOffset = m.selectedIdx - ph + 1
	}
	m.clampListScrollOffset()
}

func (m *model) clampListScrollOffset() {
	n := len(m.items)
	max := n - m.panelHeight()
	if max < 0 {
		max = 0
	}
	if m.listScrollOffset > max {
		m.listScrollOffset = max
	}
	if m.listScrollOffset < 0 {
		m.listScrollOffset = 0
	}
}

// ─── Item rendering ───────────────────────────────────────────────────────────

func (m model) renderItem(w io.Writer, item testItem, selected bool, width int) {
	var icon string
	switch item.status {
	case statusRunning:
		icon = m.spinnerView
	case statusPass:
		icon = passStyle.Render("✓")
	case statusFail:
		icon = failStyle.Render("✗")
	case statusSkip:
		icon = skipStyle.Render("~")
	}

	maxName := width - 5 // 1(space) + 1(icon) + 2(spaces) + 1(ellipsis budget)
	if maxName < 4 {
		maxName = 4
	}
	name := item.name
	if lipgloss.Width(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	if selected {
		bg := lipgloss.Color("237")
		bgSt := lipgloss.NewStyle().Background(bg)
		content := bgSt.Render(" ") + bgSt.Render(icon) + bgSt.Render("  ") + selectedStyle.Background(bg).Render(name)
		pad := width - lipgloss.Width(content)
		if pad > 0 {
			content += bgSt.Render(strings.Repeat(" ", pad))
		}
		fmt.Fprint(w, content)
	} else {
		fmt.Fprintf(w, " %s  %s", icon, name)
	}
}

// ─── Log viewport helpers ─────────────────────────────────────────────────────

func (m model) activeLogLines() []string {
	if m.selectedTest != "" {
		return m.testLogs[m.selectedTest]
	}
	return m.logLines
}

func (m model) buildLogContent() string {
	rw := m.rightWidth()
	var sb strings.Builder
	for i, line := range m.activeLogLines() {
		if lipgloss.Width(line) > rw {
			line = lipgloss.NewStyle().MaxWidth(rw).Render(line)
		}
		if i == m.logHighlightLine {
			line = logHighlight.Width(rw).Render(line)
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (m *model) rebuildLogVP() {
	if !m.vpReady {
		return
	}
	m.logVP.SetContent(m.buildLogContent())
	m.logVP.GotoBottom()
}

func (m *model) appendLog(test, line string) {
	m.logLines = append(m.logLines, line)
	m.logLineTests = append(m.logLineTests, test)
	if test != "" {
		m.testLogs[test] = append(m.testLogs[test], line)
	}
	if !m.vpReady {
		return
	}
	if m.selectedTest != "" && m.selectedTest != test {
		return
	}
	wasAtBottom := m.logVP.AtBottom()
	m.logVP.SetContent(m.buildLogContent())
	if wasAtBottom {
		m.logVP.GotoBottom()
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.X < m.leftWidth() {
			if mouse.Button == tea.MouseWheelUp {
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
			} else if mouse.Button == tea.MouseWheelDown {
				if m.selectedIdx < len(m.items)-1 {
					m.selectedIdx++
				}
			}
			m.scrollListToCursor()
			m.syncSelectedTest()
			m.logHighlightLine = -1
			m.rebuildLogVP()
			m.pinned = len(m.items) > 0 && m.selectedIdx == len(m.items)-1
		} else if m.vpReady {
			var cmd tea.Cmd
			m.logVP, cmd = m.logVP.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		inLeft := mouse.X < m.leftWidth()
		if inLeft && mouse.Button == tea.MouseLeft && !m.filterActive {
			// Rows 0-1 = header; rows 2+ = list items.
			row := mouse.Y - 2
			idx := m.listScrollOffset + row
			if idx >= 0 && idx < len(m.items) {
				m.selectedIdx = idx
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
			}
		} else if !inLeft && m.vpReady && mouse.Button == tea.MouseLeft && m.selectedTest == "" {
			// Click on a log line in all-logs view: select owning test + highlight line.
			panelRow := mouse.Y - 2
			lineIdx := m.logVP.YOffset() + panelRow
			if lineIdx >= 0 && lineIdx < len(m.logLineTests) {
				if t := m.logLineTests[lineIdx]; t != "" {
					testLineIdx := 0
					for i := 0; i < lineIdx; i++ {
						if m.logLineTests[i] == t {
							testLineIdx++
						}
					}
					m.logHighlightLine = testLineIdx
					m.selectedTest = t
					m.selectItemByName(t)
					m.scrollListToCursor()
					m.rebuildLogVP()
					offset := testLineIdx - panelRow
					if offset < 0 {
						offset = 0
					}
					if max := m.logVP.TotalLineCount() - m.panelHeight(); max > 0 && offset > max {
						offset = max
					}
					m.logVP.SetYOffset(offset)
				}
			}
		}

	case tea.KeyPressMsg:
		// Filter input captures keys when active.
		if m.filterActive {
			switch msg.String() {
			case "ctrl+c":
				m.cancel()
				return m, tea.Quit
			case "esc":
				// Clear filter and close.
				m.filterInput.Reset()
				m.filterActive = false
				m.rebuildListItems()
				// Restore cursor to previously selected test (or All).
				if m.selectedTest != "" {
					m.selectItemByName(m.selectedTest)
				}
				m.scrollListToCursor()
			case "enter":
				// Confirm filter, return to navigation.
				m.filterActive = false
				m.filterInput.Blur()
				if m.selectedTest != "" {
					m.selectItemByName(m.selectedTest)
				}
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				cmds = append(cmds, cmd)
				m.rebuildListItems()
				// If selectedTest is now filtered out, fall back to All.
				if m.selectedTest != "" {
					found := false
					for _, it := range m.items {
						if it.name == m.selectedTest {
							found = true
							break
						}
					}
					if !found {
						m.selectedTest = ""
						m.selectedIdx = 0
						m.rebuildLogVP()
					}
				}
			}
			return m, tea.Batch(cmds...)
		}

		// Normal navigation keys.
		switch msg.String() {
		case "q", "ctrl+c":
			m.cancel()
			return m, tea.Quit

		case "tab":
			m.focus = 1 - m.focus
			return m, nil

		case "/":
			if m.focus == focusTests {
				m.filterActive = true
				cmds = append(cmds, m.filterInput.Focus())
				return m, tea.Batch(cmds...)
			}

		case "up", "k":
			if m.focus == focusTests {
				if m.selectedIdx > 0 {
					m.selectedIdx--
				}
				m.scrollListToCursor()
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				m.pinned = len(m.items) > 0 && m.selectedIdx == len(m.items)-1
				return m, nil
			}

		case "down", "j":
			if m.focus == focusTests {
				if m.selectedIdx < len(m.items)-1 {
					m.selectedIdx++
				}
				m.scrollListToCursor()
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				m.pinned = len(m.items) > 0 && m.selectedIdx == len(m.items)-1
				return m, nil
			}

		case "pgup", "ctrl+u":
			if m.focus == focusTests {
				m.selectedIdx -= m.panelHeight() / 2
				if m.selectedIdx < 0 {
					m.selectedIdx = 0
				}
				m.scrollListToCursor()
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				m.pinned = len(m.items) > 0 && m.selectedIdx == len(m.items)-1
				return m, nil
			}

		case "pgdown", "ctrl+d":
			if m.focus == focusTests {
				m.selectedIdx += m.panelHeight() / 2
				if m.selectedIdx >= len(m.items) {
					m.selectedIdx = len(m.items) - 1
				}
				m.scrollListToCursor()
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				m.pinned = len(m.items) > 0 && m.selectedIdx == len(m.items)-1
				return m, nil
			}

		case "g":
			if m.focus == focusTests {
				m.selectedIdx = 0
				m.scrollListToCursor()
				m.syncSelectedTest()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				m.pinned = false
				return m, nil
			}

		case "G":
			if m.focus == focusTests {
				if len(m.items) > 0 {
					m.selectedIdx = len(m.items) - 1
					m.scrollListToCursor()
					m.syncSelectedTest()
					m.logHighlightLine = -1
					m.rebuildLogVP()
					m.pinned = true
				}
				return m, nil
			}

		case "esc":
			// Return to "All tests" from any selected test.
			if m.selectedTest != "" {
				m.selectedTest = ""
				m.selectedIdx = 0
				m.scrollListToCursor()
				m.logHighlightLine = -1
				m.rebuildLogVP()
				return m, nil
			}
		}

		if m.focus == focusLog && m.vpReady {
			var cmd tea.Cmd
			m.logVP, cmd = m.logVP.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampListScrollOffset()
		m.filterInput.SetWidth(m.leftWidth() - 2) // 2 = "/ " prefix
		m.help.SetWidth(m.rightWidth())
		if !m.vpReady {
			m.logVP = viewport.New(
				viewport.WithWidth(m.rightWidth()),
				viewport.WithHeight(m.panelHeight()),
			)
			m.logVP.SetContent(m.buildLogContent())
			m.vpReady = true
		} else {
			m.logVP.SetWidth(m.rightWidth())
			m.logVP.SetHeight(m.panelHeight())
		}

	case testEventMsg:
		if msg.action == "output" {
			if msg.output != "" {
				m.appendLog(msg.test, msg.output)
			}
		} else {
			if msg.test == "" {
				break
			}
			isNew := false
			if _, exists := m.tests[msg.test]; !exists {
				m.tests[msg.test] = &testEntry{name: msg.test}
				m.order = append(m.order, msg.test)
				isNew = true
			}
			switch msg.action {
			case "run":
				m.tests[msg.test].status = statusRunning
			case "pass":
				m.tests[msg.test].status = statusPass
			case "fail":
				m.tests[msg.test].status = statusFail
			case "skip":
				m.tests[msg.test].status = statusSkip
			}
			m.rebuildListItems()
			if isNew && m.pinned && m.selectedTest != "" {
				// Auto-scroll to newest test only when a specific test is selected.
				if len(m.items) > 0 {
					m.selectedIdx = len(m.items) - 1
					m.scrollListToCursor()
					m.syncSelectedTest()
					m.rebuildLogVP()
				}
			}
		}

	case doneMsg:
		m.done = true
		m.exitCode = msg.exitCode

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
			m.spinnerView = m.spinner.View()
		}
	}

	return m, tea.Batch(cmds...)
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() tea.View {
	v := tea.NewView(m.buildView())
	v.AltScreen = true
	if m.selectedTest != "" && m.focus == focusLog {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// padToWidth pads s to exactly w visible characters.
func padToWidth(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis < w {
		return s + strings.Repeat(" ", w-vis)
	}
	return s
}

func (m model) buildView() string {
	if m.width == 0 {
		return "Initializing..."
	}

	lw := m.leftWidth()
	rw := m.rightWidth()
	ph := m.panelHeight()
	sep := focusSep.Render("│")

	// ── Counts ──────────────────────────────────────────────────────────────
	var nPass, nFail, nSkip, nRun int
	for _, t := range m.order {
		switch m.tests[t].status {
		case statusPass:
			nPass++
		case statusFail:
			nFail++
		case statusSkip:
			nSkip++
		case statusRunning:
			nRun++
		}
	}

	// ── Left column ─────────────────────────────────────────────────────────

	// Row 1: status/spinner
	var statusStr string
	if m.done {
		if m.exitCode == 0 {
			statusStr = passStyle.Render("✓  TESTS PASSED")
		} else {
			statusStr = failStyle.Render("✗  TESTS FAILED")
		}
	} else {
		statusStr = m.spinner.View() + boldStyle.Render("  Running tests...")
	}
	leftH1 := padToWidth(" "+statusStr, lw)

	// Row 2: divider
	leftH2 := sepStyle.Render(strings.Repeat("─", lw))

	// Content: render only the visible window of items directly.
	var listLines []string
	start := m.listScrollOffset
	end := start + ph
	if end > len(m.items) {
		end = len(m.items)
	}
	for i := start; i < end; i++ {
		var sb strings.Builder
		m.renderItem(&sb, m.items[i], i == m.selectedIdx && m.selectedTest != "", lw)
		line := sb.String()
		listLines = append(listLines, padToWidth(line, lw))
	}
	// Pad with blank lines to fill the panel.
	for len(listLines) < ph {
		listLines = append(listLines, strings.Repeat(" ", lw))
	}
	leftContent := strings.Join(listLines, "\n")

	// Footer: position indicator or filter input
	var leftFooter string
	if m.filterActive {
		prefix := dimStyle.Render("/ ")
		leftFooter = prefix + m.filterInput.View()
	} else {
		n := len(m.items)
		pos := ""
		if n > 0 {
			pos = fmt.Sprintf(" %d/%d", m.selectedIdx+1, n)
		}
		if m.focus == focusTests {
			leftFooter = focusSep.Render(" [tests" + pos + "]")
		} else {
			leftFooter = dimStyle.Render(" tests" + pos)
		}
	}

	leftCol := lipgloss.JoinVertical(lipgloss.Left, leftH1, leftH2, leftContent, leftFooter)

	// ── Separator column ─────────────────────────────────────────────────────
	totalRows := ph + 3 // 2 header + ph content + 1 footer
	sepRows := make([]string, totalRows)
	for i := range sepRows {
		sepRows[i] = sep
	}
	sepCol := strings.Join(sepRows, "\n")

	// ── Right column ─────────────────────────────────────────────────────────

	// Row 1: counts
	countsStr := dimStyle.Render(fmt.Sprintf(
		"pass: %d  fail: %d  skip: %d  running: %d", nPass, nFail, nSkip, nRun,
	))
	rightH1 := " " + countsStr

	// Row 2: divider
	rightH2 := sepStyle.Render(strings.Repeat("─", rw))

	// Content: viewport
	var rightContent string
	if m.vpReady {
		rightContent = m.logVP.View()
	} else {
		rightContent = strings.Repeat("\n", ph-1)
	}

	// Footer: log label + help or done message
	logLabel := "log"
	if m.selectedTest != "" {
		logLabel = "log: " + m.selectedTest
	}
	logPct := ""
	if m.vpReady && m.logVP.TotalLineCount() > 0 {
		logPct = fmt.Sprintf(" %d%%", int(m.logVP.ScrollPercent()*100))
	}

	var rightFooter string
	if m.done {
		logNote := "logs: " + m.runDir
		if m.exitCode == 0 && !m.cfg.keepLogs {
			logNote = "logs cleaned"
		}
		rightFooter = dimStyle.Render(logNote) + "  " + focusSep.Render("q")
	} else {
		var logInfo string
		if m.focus == focusLog {
			logInfo = focusSep.Render("["+logLabel+logPct+"]") + "  "
		} else {
			logInfo = dimStyle.Render(logLabel+logPct) + "  "
		}
		m.help.SetWidth(rw - lipgloss.Width(logInfo))
		rightFooter = logInfo + m.help.View(m.keys)
	}

	rightCol := lipgloss.JoinVertical(lipgloss.Left, rightH1, rightH2, rightContent, rightFooter)

	// ── Join ─────────────────────────────────────────────────────────────────
	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, sepCol, rightCol)
}

// ─── Test runner (goroutine) ──────────────────────────────────────────────────

type goTestEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

func runTests(ctx context.Context, send func(testEventMsg), cfg config, runDir string) int {
	args := []string{"test", "-json", "-v", "./..."}
	args = append(args, cfg.goTestArgs...)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = append(os.Environ(), "TEST_OUTPUT_DIR="+runDir)

	r, w, err := os.Pipe()
	if err != nil {
		send(testEventMsg{action: "output", output: "pipe error: " + err.Error()})
		return 1
	}
	cmd.Stdout = w
	cmd.Stderr = w

	jsonLog, _ := os.Create(filepath.Join(runDir, "test_output.json"))

	if err := cmd.Start(); err != nil {
		w.Close()
		r.Close()
		if jsonLog != nil {
			jsonLog.Close()
		}
		send(testEventMsg{action: "output", output: "start error: " + err.Error()})
		return 1
	}
	w.Close()

	testLogFiles := make(map[string]*os.File)
	getTestLog := func(test string) *os.File {
		if f, ok := testLogFiles[test]; ok {
			return f
		}
		safe := strings.ReplaceAll(test, "/", "_")
		dir := filepath.Join(runDir, safe)
		os.MkdirAll(dir, 0755)
		f, _ := os.Create(filepath.Join(dir, "test_output.log"))
		testLogFiles[test] = f
		return f
	}
	defer func() {
		for _, f := range testLogFiles {
			if f != nil {
				f.Close()
			}
		}
		if jsonLog != nil {
			jsonLog.Close()
		}
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if jsonLog != nil {
			jsonLog.WriteString(line + "\n")
		}

		var evt goTestEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			send(testEventMsg{action: "output", output: line})
			continue
		}

		switch evt.Action {
		case "output":
			out := strings.TrimRight(evt.Output, "\n\r")
			if out == "" {
				continue
			}
			if evt.Test != "" {
				if f := getTestLog(evt.Test); f != nil {
					f.WriteString(out + "\n")
				}
			}
			send(testEventMsg{action: "output", test: evt.Test, output: out})

		case "run", "pass", "fail", "skip":
			if evt.Test != "" {
				send(testEventMsg{action: evt.Action, test: evt.Test})
			}
		}
	}

	r.Close()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return 130
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return exitCode
}

// ─── Plain runner ─────────────────────────────────────────────────────────────

// runPlain implements the "run" subcommand: same log splitting as the TUI but
// streams output directly to stdout and exits with go test's exit code.
func runPlain(rawArgs []string) {
	var cfg config
	var goTestArgs []string
	for i, a := range rawArgs {
		if a == "--" {
			goTestArgs = rawArgs[i+1:]
			rawArgs = rawArgs[:i]
			break
		}
	}

	fset := flag.NewFlagSet("run", flag.ExitOnError)
	fset.StringVar(&cfg.outputDir, "output-dir", "./test_logs", "Directory for log files")
	fset.BoolVar(&cfg.keepLogs, "keep-logs", false, "Keep logs even if tests pass")
	fset.BoolVar(&cfg.clean, "clean", false, "Remove old logs before running")
	_ = fset.Parse(rawArgs)
	cfg.goTestArgs = goTestArgs

	if err := os.MkdirAll(cfg.outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating output dir: %v\n", err)
		os.Exit(1)
	}

	if cfg.clean {
		entries, _ := os.ReadDir(cfg.outputDir)
		for _, e := range entries {
			os.RemoveAll(filepath.Join(cfg.outputDir, e.Name()))
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	runDir := filepath.Join(cfg.outputDir, "run_"+timestamp)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating run dir: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	send := func(msg testEventMsg) {
		if msg.action == "output" {
			fmt.Println(msg.output)
		}
	}

	exitCode := runTests(ctx, send, cfg, runDir)

	if exitCode == 0 && !cfg.keepLogs {
		entries, _ := os.ReadDir(runDir)
		for _, e := range entries {
			if e.IsDir() {
				os.RemoveAll(filepath.Join(runDir, e.Name()))
			}
		}
		os.Remove(filepath.Join(runDir, "test_output.json"))
	}

	latestLink := filepath.Join(cfg.outputDir, "latest")
	os.Remove(latestLink)
	os.Symlink("run_"+timestamp, latestLink)

	os.Exit(exitCode)
}

// ─── Help ─────────────────────────────────────────────────────────────────────

func printHelp() {
	fmt.Print(`go-test-tui — terminal UI for running Go tests

Usage:
  go-test-tui [flags] [-- go-test-flags...]
  go-test-tui <command> [flags]

Commands:
  run     Run tests, stream output to terminal (no TUI)
  list    List tests from the last run
  help    Show this help

Flags:
  -output-dir string   Directory for log files (default "./test_logs")
  -keep-logs           Keep logs even if tests pass
  -clean               Remove old log directories before running

Go test flags:
  Everything after -- is forwarded verbatim to go test.
  Examples:
    go-test-tui -- -run TestFoo
    go-test-tui -- -run TestFoo -count 2 -parallel 4

List subcommand:
  go-test-tui list [-status failed|pass|skip] [-output-dir dir] [test-name]

  -status string       Filter by status: failed, pass, skip (default: all)
  -output-dir string   Directory for log files (default "./test_logs")

  If a test name is given, its full log output is also printed.

Keyboard shortcuts (TUI):
  ↑ / k         Move up
  ↓ / j         Move down
  pgup / ctrl+u Page up
  pgdn / ctrl+d Page down
  g / G         Jump to top / bottom
  /             Filter tests
  esc           Deselect (show combined log)
  tab           Switch focus between panels
  q / ctrl+c    Quit
`)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	// Subcommand dispatch.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		switch os.Args[1] {
		case "run":
			runPlain(os.Args[2:])
			return
		case "list":
			runListReport(os.Args[2:])
			return
		case "help":
			printHelp()
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\nRun 'go-test-tui help' for usage.\n", os.Args[1])
			os.Exit(1)
		}
	}

	// Split args at "--": everything before is for the TUI, everything after
	// is forwarded verbatim to go test.
	var cfg config
	tuiArgs := os.Args[1:]
	for i, a := range tuiArgs {
		if a == "--" {
			cfg.goTestArgs = tuiArgs[i+1:]
			tuiArgs = tuiArgs[:i]
			break
		}
	}

	flag.StringVar(&cfg.outputDir, "output-dir", "./test_logs", "Directory for log files")
	flag.BoolVar(&cfg.keepLogs, "keep-logs", false, "Keep logs even if tests pass")
	flag.BoolVar(&cfg.clean, "clean", false, "Remove old logs before running")
	flag.CommandLine.Parse(tuiArgs) //nolint:errcheck

	if err := os.MkdirAll(cfg.outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating output dir: %v\n", err)
		os.Exit(1)
	}

	if cfg.clean {
		entries, _ := os.ReadDir(cfg.outputDir)
		for _, e := range entries {
			os.RemoveAll(filepath.Join(cfg.outputDir, e.Name()))
		}
	}

	timestamp := time.Now().Format("20060102_150405")
	runDir := filepath.Join(cfg.outputDir, "run_"+timestamp)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating run dir: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := newModel(cfg, runDir, cancel)
	p := tea.NewProgram(m)

	go func() {
		exitCode := runTests(ctx, func(msg testEventMsg) { p.Send(msg) }, cfg, runDir)

		if exitCode == 0 && !cfg.keepLogs {
			entries, _ := os.ReadDir(runDir)
			for _, e := range entries {
				if e.IsDir() {
					os.RemoveAll(filepath.Join(runDir, e.Name()))
				}
			}
			os.Remove(filepath.Join(runDir, "test_output.json"))
		}

		latestLink := filepath.Join(cfg.outputDir, "latest")
		os.Remove(latestLink)
		os.Symlink("run_"+timestamp, latestLink)

		p.Send(doneMsg{exitCode: exitCode})
	}()

	finalModel, err := p.Run()
	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if fm, ok := finalModel.(model); ok {
		os.Exit(fm.exitCode)
	}
}

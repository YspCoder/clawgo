//go:build with_tui

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/YspCoder/clawgo/pkg/config"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

type tuiOptions struct {
	baseURL   string
	token     string
	session   string
	noHistory bool
}

var tuiEnabled = true

type tuiMessage struct {
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	ToolCalls []interface{} `json:"tool_calls"`
}

type tuiSession struct {
	Key string `json:"key"`
}

type tuiChatEntry struct {
	Role      string
	Content   string
	Streaming bool
}

type tuiPane struct {
	ID       int
	Session  string
	Viewport viewport.Model
	Entries  []tuiChatEntry
	Loading  bool
	Sending  bool
	ErrText  string
	Unread   bool
}

type tuiRuntime struct {
	program *tea.Program
}

func (r *tuiRuntime) send(msg tea.Msg) {
	if r == nil || r.program == nil {
		return
	}
	r.program.Send(msg)
}

type tuiHistoryLoadedMsg struct {
	PaneID   int
	Session  string
	Messages []tuiMessage
	Err      error
}

type tuiSessionsLoadedMsg struct {
	Sessions []tuiSession
	Err      error
}

type tuiChatChunkMsg struct {
	PaneID int
	Delta  string
}

type tuiChatDoneMsg struct {
	PaneID int
}

type tuiChatErrorMsg struct {
	PaneID int
	Err    error
}

type tuiModel struct {
	client  *tuiClient
	runtime *tuiRuntime
	input   textinput.Model

	width        int
	height       int
	bodyHeight   int
	sidebarWidth int

	panes         []tuiPane
	focus         int
	nextPaneID    int
	sessions      []tuiSession
	sessionCursor int
	sessionFilter string
	sessionActive map[string]time.Time
	status        string
	globalErr     string
}

func tuiCmd() {
	opts, err := parseTUIOptions(os.Args[2:])
	if err != nil {
		fmt.Printf("Error: %v\n\n", err)
		printTUIHelp()
		os.Exit(1)
	}

	client := &tuiClient{
		baseURL: strings.TrimRight(opts.baseURL, "/"),
		token:   strings.TrimSpace(opts.token),
		http: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
	if err := client.ping(); err != nil {
		fmt.Printf("Error connecting to gateway: %v\n", err)
		os.Exit(1)
	}

	runtime := &tuiRuntime{}
	model := newTUIModel(client, runtime, opts)
	program := tea.NewProgram(model, tea.WithAltScreen())
	runtime.program = program

	if _, err := program.Run(); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

func newTUIModel(client *tuiClient, runtime *tuiRuntime, opts tuiOptions) tuiModel {
	input := textinput.New()
	input.Placeholder = "Type a message for the focused pane or /help"
	input.Focus()
	input.CharLimit = 0
	input.Prompt = "› "

	pane := newTUIPane(1, opts.session)
	if opts.noHistory {
		pane.Entries = []tuiChatEntry{{Role: "system", Content: "History loading skipped. Start chatting."}}
	}

	return tuiModel{
		client:     client,
		runtime:    runtime,
		input:      input,
		panes:      []tuiPane{pane},
		focus:      0,
		nextPaneID: 2,
		sessionActive: map[string]time.Time{
			strings.TrimSpace(opts.session): time.Now(),
		},
		status: "Tab/Shift+Tab: focus • Ctrl+O: open selected session • Ctrl+R: replace pane • Enter: send • Ctrl+C: quit",
	}
}

func newTUIPane(id int, session string) tuiPane {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return tuiPane{
		ID:       id,
		Session:  strings.TrimSpace(session),
		Viewport: vp,
	}
}

func (m tuiModel) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink}
	cmds = append(cmds, m.loadSessionsCmd())
	for _, pane := range m.panes {
		if len(pane.Entries) == 0 {
			cmds = append(cmds, m.loadHistoryCmd(pane.ID, pane.Session))
		}
	}
	return tea.Batch(cmds...)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+w":
			if len(m.panes) <= 1 {
				m.globalErr = "Cannot close the last pane"
				return m, nil
			}
			closed := m.panes[m.focus].Session
			m.panes = append(m.panes[:m.focus], m.panes[m.focus+1:]...)
			if m.focus >= len(m.panes) {
				m.focus = len(m.panes) - 1
			}
			m.globalErr = ""
			m.status = "Closed pane: " + closed
			m.resize()
			return m, nil
		case "ctrl+l":
			if len(m.panes) == 0 {
				return m, nil
			}
			m.panes[m.focus].Loading = true
			m.panes[m.focus].ErrText = ""
			m.globalErr = ""
			m.status = "Reloading history: " + m.panes[m.focus].Session
			return m, m.loadHistoryCmd(m.panes[m.focus].ID, m.panes[m.focus].Session)
		case "esc":
			if strings.TrimSpace(m.sessionFilter) != "" {
				m.sessionFilter = ""
				filtered := m.filteredSessions()
				if len(filtered) == 0 {
					m.sessionCursor = 0
				} else if m.sessionCursor >= len(filtered) {
					m.sessionCursor = len(filtered) - 1
				}
				m.globalErr = ""
				m.status = "Session filter cleared"
				return m, nil
			}
		case "tab":
			if len(m.panes) > 1 {
				m.focus = (m.focus + 1) % len(m.panes)
				m.panes[m.focus].Unread = false
				m.markSessionActive(m.focusedPane().Session)
				m.status = "Focused pane: " + m.focusedPane().Session
			}
			return m, nil
		case "alt+left":
			if len(m.panes) > 1 {
				m.focus = (m.focus - 1 + len(m.panes)) % len(m.panes)
				m.panes[m.focus].Unread = false
				m.markSessionActive(m.focusedPane().Session)
				m.status = "Focused pane: " + m.focusedPane().Session
			}
			return m, nil
		case "alt+right":
			if len(m.panes) > 1 {
				m.focus = (m.focus + 1) % len(m.panes)
				m.panes[m.focus].Unread = false
				m.markSessionActive(m.focusedPane().Session)
				m.status = "Focused pane: " + m.focusedPane().Session
			}
			return m, nil
		case "up":
			filtered := m.filteredSessions()
			if len(filtered) > 0 && m.sessionCursor > 0 {
				m.sessionCursor--
				m.status = "Selected session: " + filtered[m.sessionCursor].Key
			}
			return m, nil
		case "down":
			filtered := m.filteredSessions()
			if len(filtered) > 0 && m.sessionCursor < len(filtered)-1 {
				m.sessionCursor++
				m.status = "Selected session: " + filtered[m.sessionCursor].Key
			}
			return m, nil
		case "shift+tab":
			if len(m.panes) > 1 {
				m.focus = (m.focus - 1 + len(m.panes)) % len(m.panes)
				m.status = "Focused pane: " + m.focusedPane().Session
			}
			return m, nil
		case "alt+1", "alt+2", "alt+3", "alt+4":
			target := 0
			switch msg.String() {
			case "alt+2":
				target = 1
			case "alt+3":
				target = 2
			case "alt+4":
				target = 3
			}
			if target < len(m.panes) {
				m.focus = target
				m.panes[m.focus].Unread = false
				m.status = "Focused pane: " + m.focusedPane().Session
			}
			return m, nil
		case "ctrl+o":
			return m.openSelectedSessionInPane(false)
		case "ctrl+n":
			return m.openSelectedSessionInPane(false)
		case "ctrl+r":
			return m.openSelectedSessionInPane(true)
		case "pgdown", "pgup", "home", "end":
			if len(m.panes) > 0 {
				var cmd tea.Cmd
				pane := &m.panes[m.focus]
				pane.Viewport, cmd = pane.Viewport.Update(msg)
				return m, cmd
			}
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m.openSelectedSessionInPane(false)
			}
			if len(m.panes) == 0 || m.focusedPane().Sending || m.focusedPane().Loading {
				return m, nil
			}
			m.input.SetValue("")
			if strings.HasPrefix(value, "/") {
				return m.handleCommand(value)
			}
			return m.startSend(value)
		}
	case tuiHistoryLoadedMsg:
		idx := m.findPane(msg.PaneID)
		if idx < 0 {
			return m, nil
		}
		pane := &m.panes[idx]
		pane.Loading = false
		if msg.Err != nil {
			pane.ErrText = msg.Err.Error()
			m.status = "History load failed for " + pane.Session
			return m, nil
		}
		pane.Session = msg.Session
		m.markSessionActive(pane.Session)
		pane.Entries = historyEntries(msg.Messages)
		if len(pane.Entries) == 0 {
			pane.Entries = []tuiChatEntry{{Role: "system", Content: "No history in this session yet."}}
		}
		pane.ErrText = ""
		m.status = "Session loaded: " + pane.Session
		m.syncPaneViewport(idx, true)
		return m, nil
	case tuiSessionsLoadedMsg:
		if msg.Err != nil {
			m.globalErr = msg.Err.Error()
			return m, nil
		}
		m.sessions = filterTUISessions(msg.Sessions)
		if len(m.sessions) == 0 {
			m.sessions = []tuiSession{{Key: "main"}}
		}
		filtered := m.filteredSessions()
		if len(filtered) == 0 {
			m.sessionCursor = 0
		} else if m.sessionCursor >= len(filtered) {
			m.sessionCursor = len(filtered) - 1
		}
		keys := make([]string, 0, len(msg.Sessions))
		for _, session := range m.sessions {
			if strings.TrimSpace(session.Key) != "" {
				keys = append(keys, session.Key)
			}
		}
		m.appendSystemToFocused("Sessions: " + strings.Join(keys, ", "))
		m.globalErr = ""
		m.status = fmt.Sprintf("%d sessions", len(keys))
		return m, nil
	case tuiChatChunkMsg:
		idx := m.findPane(msg.PaneID)
		if idx < 0 {
			return m, nil
		}
		pane := &m.panes[idx]
		if len(pane.Entries) == 0 || !pane.Entries[len(pane.Entries)-1].Streaming {
			pane.Entries = append(pane.Entries, tuiChatEntry{Role: "assistant", Streaming: true})
		}
		pane.Entries[len(pane.Entries)-1].Content += msg.Delta
		if idx != m.focus {
			pane.Unread = true
		}
		m.markSessionActive(pane.Session)
		m.syncPaneViewport(idx, true)
		return m, nil
	case tuiChatDoneMsg:
		idx := m.findPane(msg.PaneID)
		if idx < 0 {
			return m, nil
		}
		pane := &m.panes[idx]
		pane.Sending = false
		if len(pane.Entries) > 0 && pane.Entries[len(pane.Entries)-1].Streaming {
			pane.Entries[len(pane.Entries)-1].Streaming = false
		}
		pane.ErrText = ""
		if idx == m.focus {
			pane.Unread = false
		}
		m.markSessionActive(pane.Session)
		m.status = "Reply complete: " + pane.Session
		m.syncPaneViewport(idx, true)
		return m, nil
	case tuiChatErrorMsg:
		idx := m.findPane(msg.PaneID)
		if idx < 0 {
			return m, nil
		}
		pane := &m.panes[idx]
		pane.Sending = false
		if len(pane.Entries) > 0 && pane.Entries[len(pane.Entries)-1].Streaming {
			pane.Entries[len(pane.Entries)-1].Streaming = false
		}
		pane.ErrText = msg.Err.Error()
		m.status = "Reply failed: " + pane.Session
		m.syncPaneViewport(idx, true)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tuiModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}
	banner := renderTUIBanner(m.width)
	focusSession := ""
	if len(m.panes) > 0 {
		focusSession = m.focusedPane().Session
	}
	header := tuiHeaderStyle.Width(m.width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			banner,
			tuiMutedStyle.Render(fmt.Sprintf("Focus %s  •  Panes %d  •  %s", focusSession, len(m.panes), compactGatewayLabel(m.client.baseURL))),
		),
	)
	body := m.renderPanes()
	input := tuiInputBoxStyle.Width(m.width).Render(m.input.View())
	footerText := m.status
	if m.globalErr != "" {
		footerText = "Error: " + m.globalErr
	}
	footerStyle := tuiFooterStyle
	if m.globalErr != "" {
		footerStyle = tuiErrorStyle
	}
	footerLeft := footerStyle.Render(footerText)
	footerRight := tuiFooterHintStyle.Render("Enter send  •  Ctrl+N open  •  Ctrl+W close  •  Alt+1..4 focus")
	footer := footerStyle.Width(m.width).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			footerLeft,
			strings.Repeat(" ", max(1, m.width-lipgloss.Width(footerLeft)-lipgloss.Width(footerRight)-2)),
			footerRight,
		),
	)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, input, footer)
}

func (m *tuiModel) resize() {
	headerHeight := lipgloss.Height(
		tuiHeaderStyle.Width(max(m.width, 20)).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				renderTUIBanner(max(m.width, 20)),
				tuiMutedStyle.Render(fmt.Sprintf("Gateway %s", m.client.baseURL)),
			),
		),
	)
	inputHeight := 3
	footerHeight := 2
	bodyHeight := m.height - headerHeight - inputHeight - footerHeight
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	m.bodyHeight = bodyHeight
	m.sidebarWidth = min(28, max(20, m.width/5))
	availablePaneWidth := m.width - m.sidebarWidth - 1
	if availablePaneWidth < 24 {
		availablePaneWidth = 24
	}
	switch len(m.panes) {
	case 0:
	case 1:
		m.setPaneSize(0, availablePaneWidth, bodyHeight)
	case 2:
		leftWidth := (availablePaneWidth - 1) / 2
		rightWidth := availablePaneWidth - 1 - leftWidth
		m.setPaneSize(0, leftWidth, bodyHeight)
		m.setPaneSize(1, rightWidth, bodyHeight)
	case 3:
		leftWidth := (availablePaneWidth - 1) / 2
		rightWidth := availablePaneWidth - 1 - leftWidth
		topHeight := (bodyHeight - 1) / 2
		bottomHeight := bodyHeight - 1 - topHeight
		m.setPaneSize(0, leftWidth, bodyHeight)
		m.setPaneSize(1, rightWidth, topHeight)
		m.setPaneSize(2, rightWidth, bottomHeight)
	default:
		leftWidth := (availablePaneWidth - 1) / 2
		rightWidth := availablePaneWidth - 1 - leftWidth
		topHeight := (bodyHeight - 1) / 2
		bottomHeight := bodyHeight - 1 - topHeight
		m.setPaneSize(0, leftWidth, topHeight)
		m.setPaneSize(1, rightWidth, topHeight)
		m.setPaneSize(2, leftWidth, bottomHeight)
		m.setPaneSize(3, rightWidth, bottomHeight)
	}
	m.input.Width = max(10, m.width-6)
}

func (m *tuiModel) renderPanes() string {
	sidebar := m.renderSessionSidebar()
	panes := m.renderPaneGrid()
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, tuiPaneGapStyle.Render(" "), panes)
}

func (m *tuiModel) renderPane(index int, pane tuiPane) string {
	titleStyle := tuiPaneTitleStyle
	borderStyle := tuiPaneStyle
	if index == m.focus {
		titleStyle = tuiPaneTitleActiveStyle
		borderStyle = tuiPaneActiveStyle
	}
	statusBadge := ""
	switch {
	case pane.Sending:
		statusBadge = tuiPaneReplyingStyle.Render(" replying ")
	case pane.Loading:
		statusBadge = tuiPaneLoadingStyle.Render(" loading ")
	case pane.ErrText != "":
		statusBadge = tuiPaneErrorBadgeStyle.Render(" error ")
	}
	titleText := fmt.Sprintf("[%d] %s", index+1, pane.Session)
	if pane.Unread {
		titleText += " •"
	}
	title := titleStyle.Render(titleText)
	if index == m.focus {
		title = tuiPaneTitleActiveBadgeStyle.Render(" " + titleText + " ")
	}
	meta := []string{}
	if pane.Loading {
		meta = append(meta, "loading")
	}
	if pane.Sending {
		meta = append(meta, "replying")
	}
	if pane.ErrText != "" {
		meta = append(meta, "error")
	}
	header := title
	if statusBadge != "" {
		header = lipgloss.JoinHorizontal(lipgloss.Left, header, " ", statusBadge)
	}
	if len(meta) > 0 {
		header = lipgloss.JoinHorizontal(lipgloss.Left, title, tuiMutedStyle.Render("  "+strings.Join(meta, " • ")))
		if statusBadge != "" {
			header = lipgloss.JoinHorizontal(lipgloss.Left, title, " ", statusBadge, tuiMutedStyle.Render("  "+strings.Join(meta, " • ")))
		}
	}
	content := pane.Viewport.View()
	if pane.ErrText != "" {
		content += "\n\n" + tuiErrorStyle.Render("Error: "+pane.ErrText)
	}
	view := lipgloss.JoinVertical(lipgloss.Left, header, content)
	return borderStyle.Width(pane.Viewport.Width + 4).Height(pane.Viewport.Height + 4).Render(view)
}

func (m *tuiModel) syncPaneViewport(index int, toBottom bool) {
	if index < 0 || index >= len(m.panes) {
		return
	}
	pane := &m.panes[index]
	pane.Viewport.SetContent(renderEntries(pane.Entries, pane.Viewport.Width))
	if toBottom {
		pane.Viewport.GotoBottom()
	}
}

func (m *tuiModel) renderSessionSidebar() string {
	lines := []string{tuiPaneTitleStyle.Render("Sessions")}
	filtered := m.filteredSessions()
	if len(filtered) == 0 {
		lines = append(lines, tuiMutedStyle.Render("No sessions"))
	} else {
		for i, session := range filtered {
			style := tuiSidebarItemStyle
			prefix := "  "
			suffix := m.sidebarSessionMarker(session.Key)
			if i == m.sessionCursor {
				style = tuiSidebarItemActiveStyle
				prefix = "› "
			}
			lines = append(lines, style.Width(max(8, m.sidebarWidth-4)).Render(prefix+session.Key+suffix))
		}
	}
	lines = append(lines, "")
	if strings.TrimSpace(m.sessionFilter) != "" {
		lines = append(lines, tuiMutedStyle.Render("Filter: "+m.sessionFilter))
	}
	lines = append(lines, tuiMutedStyle.Render("Up/Down: select"))
	lines = append(lines, tuiMutedStyle.Render("Enter: open selected"))
	lines = append(lines, tuiMutedStyle.Render("Ctrl+O: open pane"))
	lines = append(lines, tuiMutedStyle.Render("Ctrl+N: new pane"))
	lines = append(lines, tuiMutedStyle.Render("Ctrl+R: replace pane"))
	lines = append(lines, tuiMutedStyle.Render("Ctrl+L: reload pane"))
	lines = append(lines, tuiMutedStyle.Render("Ctrl+W: close pane"))
	lines = append(lines, tuiMutedStyle.Render("Alt+←/→: cycle pane"))
	lines = append(lines, tuiMutedStyle.Render("Alt+1..4: focus pane"))
	lines = append(lines, tuiMutedStyle.Render("Esc: clear filter"))
	lines = append(lines, tuiMutedStyle.Render("/filter <text>: filter"))
	return tuiSidebarStyle.Width(m.sidebarWidth).Height(m.bodyHeight).Render(strings.Join(lines, "\n"))
}

func (m *tuiModel) renderPaneGrid() string {
	if len(m.panes) == 0 {
		return tuiViewportStyle.Width(max(20, m.width-m.sidebarWidth-1)).Height(m.bodyHeight).Render("No panes.")
	}
	switch len(m.panes) {
	case 1:
		return m.renderPane(0, m.panes[0])
	case 2:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderPane(0, m.panes[0]),
			tuiPaneGapStyle.Render(" "),
			m.renderPane(1, m.panes[1]),
		)
	case 3:
		right := lipgloss.JoinVertical(lipgloss.Left,
			m.renderPane(1, m.panes[1]),
			tuiPaneGapStyle.Render(" "),
			m.renderPane(2, m.panes[2]),
		)
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderPane(0, m.panes[0]),
			tuiPaneGapStyle.Render(" "),
			right,
		)
	default:
		top := lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderPane(0, m.panes[0]),
			tuiPaneGapStyle.Render(" "),
			m.renderPane(1, m.panes[1]),
		)
		bottom := lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderPane(2, m.panes[2]),
			tuiPaneGapStyle.Render(" "),
			m.renderPane(3, m.panes[3]),
		)
		return lipgloss.JoinVertical(lipgloss.Left,
			top,
			tuiPaneGapStyle.Render(" "),
			bottom,
		)
	}
}

func (m tuiModel) loadHistoryCmd(paneID int, session string) tea.Cmd {
	return func() tea.Msg {
		messages, err := m.client.history(context.Background(), session)
		return tuiHistoryLoadedMsg{PaneID: paneID, Session: session, Messages: messages, Err: err}
	}
}

func (m tuiModel) loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.client.sessions(context.Background())
		return tuiSessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

func (m tuiModel) startSend(content string) (tea.Model, tea.Cmd) {
	if len(m.panes) == 0 {
		return m, nil
	}
	pane := &m.panes[m.focus]
	pane.Entries = append(pane.Entries,
		tuiChatEntry{Role: "user", Content: content},
		tuiChatEntry{Role: "assistant", Streaming: true},
	)
	pane.Sending = true
	pane.ErrText = ""
	m.markSessionActive(pane.Session)
	m.status = "Waiting for reply: " + pane.Session
	m.syncPaneViewport(m.focus, true)
	return m, m.streamChatCmd(pane.ID, pane.Session, content)
}

func (m tuiModel) handleCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	cmd := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	switch cmd {
	case "quit", "exit":
		return m, tea.Quit
	case "help":
		m.appendSystemToFocused("Commands: /help, /sessions, /split <session>, /close, /session <name>, /focus <n>, /history, /clear, /filter <text>, /quit")
		m.status = "Help shown"
		return m, nil
	case "sessions":
		m.status = "Loading sessions..."
		return m, m.loadSessionsCmd()
	case "history":
		if len(m.panes) == 0 {
			return m, nil
		}
		m.panes[m.focus].Loading = true
		m.status = "Loading history..."
		return m, m.loadHistoryCmd(m.panes[m.focus].ID, m.panes[m.focus].Session)
	case "clear":
		if len(m.panes) == 0 {
			return m, nil
		}
		m.panes[m.focus].Entries = nil
		m.panes[m.focus].ErrText = ""
		m.status = "Transcript cleared: " + m.panes[m.focus].Session
		m.syncPaneViewport(m.focus, true)
		return m, nil
	case "session":
		if len(fields) < 2 || len(m.panes) == 0 {
			m.globalErr = "/session requires a session name"
			return m, nil
		}
		m.panes[m.focus].Session = strings.TrimSpace(fields[1])
		m.markSessionActive(m.panes[m.focus].Session)
		m.panes[m.focus].Loading = true
		m.status = "Switched pane to " + m.panes[m.focus].Session
		return m, m.loadHistoryCmd(m.panes[m.focus].ID, m.panes[m.focus].Session)
	case "split", "new":
		if len(fields) < 2 {
			m.globalErr = fmt.Sprintf("/%s requires a session name", cmd)
			return m, nil
		}
		if len(m.panes) >= 4 {
			m.globalErr = "Maximum 4 panes"
			return m, nil
		}
		session := strings.TrimSpace(fields[1])
		pane := newTUIPane(m.nextPaneID, session)
		m.nextPaneID++
		pane.Loading = true
		m.panes = append(m.panes, pane)
		m.focus = len(m.panes) - 1
		m.globalErr = ""
		m.status = "Opened pane: " + session
		m.resize()
		return m, tea.Batch(m.loadHistoryCmd(pane.ID, pane.Session), m.loadSessionsCmd())
	case "close":
		if len(m.panes) <= 1 {
			m.globalErr = "Cannot close the last pane"
			return m, nil
		}
		closed := m.panes[m.focus].Session
		m.panes = append(m.panes[:m.focus], m.panes[m.focus+1:]...)
		if m.focus >= len(m.panes) {
			m.focus = len(m.panes) - 1
		}
		m.globalErr = ""
		m.status = "Closed pane: " + closed
		m.resize()
		return m, nil
	case "focus":
		if len(fields) < 2 {
			m.globalErr = "/focus requires a pane number"
			return m, nil
		}
		var n int
		if _, err := fmt.Sscanf(fields[1], "%d", &n); err != nil || n < 1 || n > len(m.panes) {
			m.globalErr = "Invalid pane number"
			return m, nil
		}
		m.focus = n - 1
		m.panes[m.focus].Unread = false
		m.markSessionActive(m.panes[m.focus].Session)
		m.globalErr = ""
		m.status = "Focused pane: " + m.panes[m.focus].Session
		return m, nil
	case "filter":
		value := ""
		if len(fields) > 1 {
			value = strings.TrimSpace(strings.Join(fields[1:], " "))
		}
		m.sessionFilter = value
		filtered := m.filteredSessions()
		if len(filtered) == 0 {
			m.sessionCursor = 0
			m.globalErr = "No matching sessions"
		} else {
			m.sessionCursor = 0
			m.globalErr = ""
			m.status = fmt.Sprintf("Filtered sessions: %d", len(filtered))
		}
		return m, nil
	default:
		m.globalErr = "Unknown command: " + input
		return m, nil
	}
}

func (m tuiModel) streamChatCmd(paneID int, session, content string) tea.Cmd {
	client := m.client
	runtime := m.runtime
	return func() tea.Msg {
		go func() {
			err := client.streamChat(context.Background(), session, content, func(msg tea.Msg) {
				switch typed := msg.(type) {
				case tuiChatChunkMsg:
					typed.PaneID = paneID
					runtime.send(typed)
				default:
					runtime.send(msg)
				}
			})
			if err != nil {
				runtime.send(tuiChatErrorMsg{PaneID: paneID, Err: err})
			}
		}()
		return nil
	}
}

func (m *tuiModel) appendSystemToFocused(content string) {
	if len(m.panes) == 0 {
		return
	}
	m.panes[m.focus].Entries = append(m.panes[m.focus].Entries, tuiChatEntry{Role: "system", Content: content})
	m.syncPaneViewport(m.focus, true)
}

func (m *tuiModel) setPaneSize(index, width, height int) {
	if index < 0 || index >= len(m.panes) {
		return
	}
	if width < 24 {
		width = 24
	}
	if height < 6 {
		height = 6
	}
	m.panes[index].Viewport.Width = width - 4
	m.panes[index].Viewport.Height = height - 4
	if m.panes[index].Viewport.Height < 3 {
		m.panes[index].Viewport.Height = 3
	}
	m.syncPaneViewport(index, false)
}

func (m *tuiModel) hasPaneSession(session string) bool {
	needle := strings.TrimSpace(session)
	if needle == "" {
		return false
	}
	for i := range m.panes {
		if strings.TrimSpace(m.panes[i].Session) == needle {
			return true
		}
	}
	return false
}

func (m *tuiModel) hasUnreadSession(session string) bool {
	needle := strings.TrimSpace(session)
	if needle == "" {
		return false
	}
	for i := range m.panes {
		if strings.TrimSpace(m.panes[i].Session) == needle && m.panes[i].Unread {
			return true
		}
	}
	return false
}

func (m *tuiModel) sidebarSessionMarker(session string) string {
	focused := m.focusedPane() != nil && strings.TrimSpace(m.focusedPane().Session) == strings.TrimSpace(session)
	open := m.hasPaneSession(session)
	unread := m.hasUnreadSession(session)
	switch {
	case focused && unread:
		return " ◉•"
	case focused:
		return " ◉"
	case unread:
		return " •"
	case open:
		return " ●"
	default:
		return ""
	}
}

func (m tuiModel) openSelectedSessionInPane(replace bool) (tea.Model, tea.Cmd) {
	filtered := m.filteredSessions()
	if len(filtered) == 0 {
		m.globalErr = "No sessions available"
		return m, nil
	}
	if m.sessionCursor >= len(filtered) {
		m.sessionCursor = len(filtered) - 1
	}
	session := strings.TrimSpace(filtered[m.sessionCursor].Key)
	if session == "" {
		m.globalErr = "Invalid session"
		return m, nil
	}
	if idx := m.findPaneBySession(session); idx >= 0 && !replace {
		m.focus = idx
		m.panes[m.focus].Unread = false
		m.markSessionActive(session)
		m.globalErr = ""
		m.status = "Focused pane: " + session
		return m, nil
	}
	if replace {
		if len(m.panes) == 0 {
			return m, nil
		}
		m.panes[m.focus].Session = session
		m.markSessionActive(session)
		m.panes[m.focus].Loading = true
		m.panes[m.focus].ErrText = ""
		m.status = "Replaced pane with " + session
		return m, m.loadHistoryCmd(m.panes[m.focus].ID, session)
	}
	if len(m.panes) >= 4 {
		m.globalErr = "Maximum 4 panes"
		return m, nil
	}
	pane := newTUIPane(m.nextPaneID, session)
	m.nextPaneID++
	pane.Loading = true
	m.panes = append(m.panes, pane)
	m.focus = len(m.panes) - 1
	m.markSessionActive(session)
	m.globalErr = ""
	m.status = "Opened pane: " + session
	m.resize()
	return m, m.loadHistoryCmd(pane.ID, session)
}

func (m *tuiModel) findPane(id int) int {
	for i := range m.panes {
		if m.panes[i].ID == id {
			return i
		}
	}
	return -1
}

func (m *tuiModel) findPaneBySession(session string) int {
	needle := strings.TrimSpace(session)
	for i := range m.panes {
		if strings.TrimSpace(m.panes[i].Session) == needle {
			return i
		}
	}
	return -1
}

func (m *tuiModel) filteredSessions() []tuiSession {
	out := make([]tuiSession, 0, len(m.sessions))
	if strings.TrimSpace(m.sessionFilter) == "" {
		out = append(out, m.sessions...)
	} else {
		query := strings.ToLower(strings.TrimSpace(m.sessionFilter))
		for _, session := range m.sessions {
			if strings.Contains(strings.ToLower(session.Key), query) {
				out = append(out, session)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		iOpen := m.hasPaneSession(out[i].Key)
		jOpen := m.hasPaneSession(out[j].Key)
		if iOpen != jOpen {
			return iOpen
		}
		iActive := m.sessionActiveAt(out[i].Key)
		jActive := m.sessionActiveAt(out[j].Key)
		if !iActive.Equal(jActive) {
			return iActive.After(jActive)
		}
		return strings.ToLower(out[i].Key) < strings.ToLower(out[j].Key)
	})
	return out
}

func (m *tuiModel) markSessionActive(session string) {
	key := strings.TrimSpace(session)
	if key == "" {
		return
	}
	if m.sessionActive == nil {
		m.sessionActive = map[string]time.Time{}
	}
	m.sessionActive[key] = time.Now()
}

func (m *tuiModel) sessionActiveAt(session string) time.Time {
	if m.sessionActive == nil {
		return time.Time{}
	}
	return m.sessionActive[strings.TrimSpace(session)]
}

func (m *tuiModel) focusedPane() *tuiPane {
	if len(m.panes) == 0 {
		return nil
	}
	return &m.panes[m.focus]
}

func parseTUIOptions(args []string) (tuiOptions, error) {
	opts := tuiOptions{session: "main"}
	cfg, err := loadConfig()
	if err == nil {
		opts.baseURL = gatewayBaseURLFromConfig(cfg)
		opts.token = strings.TrimSpace(cfg.Gateway.Token)
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--help", "-h":
			printTUIHelp()
			os.Exit(0)
		case "--gateway":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--gateway requires a value")
			}
			opts.baseURL = strings.TrimSpace(args[i+1])
			i++
		case "--token":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--token requires a value")
			}
			opts.token = strings.TrimSpace(args[i+1])
			i++
		case "--session", "-s":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--session requires a value")
			}
			opts.session = strings.TrimSpace(args[i+1])
			i++
		case "--no-history":
			opts.noHistory = true
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if strings.TrimSpace(opts.baseURL) == "" {
		opts.baseURL = "http://127.0.0.1:8080"
	}
	if !strings.HasPrefix(opts.baseURL, "http://") && !strings.HasPrefix(opts.baseURL, "https://") {
		opts.baseURL = "http://" + opts.baseURL
	}
	if strings.TrimSpace(opts.session) == "" {
		opts.session = "main"
	}
	return opts, nil
}

func printTUIHelp() {
	fmt.Println("Usage: clawgo tui [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --gateway <url>       Gateway base URL, for example http://127.0.0.1:8080")
	fmt.Println("  --token <value>       Gateway token")
	fmt.Println("  --session, -s <key>   Initial session key (default: main)")
	fmt.Println("  --no-history          Skip loading session history on startup")
	fmt.Println()
	fmt.Println("In TUI:")
	fmt.Println("  /split <session>      Open a new pane for another session")
	fmt.Println("  /close                Close focused pane")
	fmt.Println("  /focus <n>            Focus pane number")
}

func gatewayBaseURLFromConfig(cfg *config.Config) string {
	host := strings.TrimSpace(cfg.Gateway.Host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", host, cfg.Gateway.Port)
}

func historyEntries(messages []tuiMessage) []tuiChatEntry {
	out := make([]tuiChatEntry, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" && len(message.ToolCalls) > 0 {
			content = fmt.Sprintf("[tool calls: %d]", len(message.ToolCalls))
		}
		if content == "" {
			continue
		}
		out = append(out, tuiChatEntry{Role: strings.ToLower(strings.TrimSpace(message.Role)), Content: content})
	}
	return out
}

func renderEntries(entries []tuiChatEntry, width int) string {
	if len(entries) == 0 {
		return tuiMutedStyle.Render("No messages yet.")
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		label := tuiRoleLabel(entry.Role)
		content := entry.Content
		if strings.TrimSpace(content) == "" && entry.Streaming {
			content = "..."
		}
		style := tuiContentStyle
		switch entry.Role {
		case "user":
			style = tuiUserStyle
		case "assistant":
			style = tuiAssistantStyle
		case "system":
			style = tuiSystemStyle
		case "tool":
			style = tuiToolStyle
		}
		if width > 8 {
			style = style.Width(width - 2)
		}
		if entry.Streaming {
			content += " ▌"
		}
		parts = append(parts, lipgloss.JoinVertical(lipgloss.Left, tuiLabelStyle.Render(label), style.Render(content)))
	}
	return strings.Join(parts, "\n\n")
}

func tuiRoleLabel(role string) string {
	switch role {
	case "user":
		return "You"
	case "system":
		return "System"
	case "tool":
		return "Tool"
	default:
		return "Assistant"
	}
}

type tuiClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func (c *tuiClient) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := c.newRequest(ctx, http.MethodGet, "/webui/api/version", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *tuiClient) history(ctx context.Context, session string) ([]tuiMessage, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/webui/api/chat/history?session="+url.QueryEscape(session), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var payload struct {
		Messages []tuiMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Messages, nil
}

func (c *tuiClient) sessions(ctx context.Context) ([]tuiSession, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/webui/api/sessions", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}
	var payload struct {
		Sessions []tuiSession `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Sessions, nil
}

func (c *tuiClient) streamChat(ctx context.Context, session, content string, send func(tea.Msg)) error {
	conn, err := c.openChatSocket(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"session": session, "message": content}); err != nil {
		return err
	}
	for {
		var frame struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Error string `json:"error"`
		}
		if err := conn.ReadJSON(&frame); err != nil {
			return err
		}
		switch frame.Type {
		case "chat_chunk":
			send(tuiChatChunkMsg{Delta: frame.Delta})
		case "chat_done":
			send(tuiChatDoneMsg{})
			return nil
		case "chat_error":
			return fmt.Errorf("%s", strings.TrimSpace(frame.Error))
		}
	}
}

func (c *tuiClient) openChatSocket(ctx context.Context) (*websocket.Conn, error) {
	wsURL, err := c.websocketURL("/webui/api/chat/live")
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	if c.token != "" {
		header.Set("Authorization", "Bearer "+c.token)
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		if resp != nil && resp.Body != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			if strings.TrimSpace(string(body)) != "" {
				return nil, fmt.Errorf("%s", strings.TrimSpace(string(body)))
			}
		}
		return nil, err
	}
	return conn, nil
}

func (c *tuiClient) websocketURL(path string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported gateway scheme: %s", u.Scheme)
	}
	u.Path = path
	q := u.Query()
	if c.token != "" {
		q.Set("token", c.token)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *tuiClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

var (
	tuiHeaderStyle = lipgloss.NewStyle().
			Padding(0, 1).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))

	tuiTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212"))

	tuiBannerClawStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("69"))

	tuiBannerGoStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("204"))

	tuiMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	tuiInputBoxStyle = lipgloss.NewStyle().
				Padding(0, 1).
				BorderTop(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("240"))

	tuiViewportStyle = lipgloss.NewStyle().Padding(0, 1)

	tuiFooterStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("245"))

	tuiErrorStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("203"))

	tuiPaneStyle = lipgloss.NewStyle().
			Padding(0, 1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	tuiPaneActiveStyle = lipgloss.NewStyle().
				Padding(0, 1).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("69"))

	tuiPaneTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("252"))

	tuiPaneTitleActiveStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("69"))

	tuiPaneTitleActiveBadgeStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("16")).
					Background(lipgloss.Color("69"))

	tuiPaneGapStyle = lipgloss.NewStyle()

	tuiSidebarStyle = lipgloss.NewStyle().
			Padding(0, 1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	tuiSidebarItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252"))

	tuiSidebarItemActiveStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("69"))
	tuiFooterHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	tuiPaneReplyingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("230")).
				Background(lipgloss.Color("62"))

	tuiPaneLoadingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("16")).
				Background(lipgloss.Color("221"))

	tuiPaneErrorBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("255")).
				Background(lipgloss.Color("160"))

	tuiLabelStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("249"))

	tuiContentStyle   = lipgloss.NewStyle().PaddingLeft(1)
	tuiUserStyle      = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color("87"))
	tuiAssistantStyle = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(lipgloss.Color("230"))
	tuiSystemStyle = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color("221"))
	tuiToolStyle   = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color("141"))
)

func renderTUIBanner(width int) string {
	claw := strings.Join([]string{
		" ██████╗██╗      █████╗ ██╗    ██╗",
		"██╔════╝██║     ██╔══██╗██║    ██║",
		"██║     ██║     ███████║██║ █╗ ██║",
		"██║     ██║     ██╔══██║██║███╗██║",
		"╚██████╗███████╗██║  ██║╚███╔███╔╝",
		" ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝ ",
	}, "\n")
	goText := strings.Join([]string{
		" ██████╗  ██████╗ ",
		"██╔════╝ ██╔═══██╗",
		"██║  ███╗██║   ██║",
		"██║   ██║██║   ██║",
		"╚██████╔╝╚██████╔╝",
		" ╚═════╝  ╚═════╝ ",
	}, "\n")
	left := strings.Split(claw, "\n")
	right := strings.Split(goText, "\n")
	lines := make([]string, 0, len(left))
	for i := range left {
		lines = append(lines, tuiBannerClawStyle.Render(left[i])+"  "+tuiBannerGoStyle.Render(right[i]))
	}
	full := strings.Join(lines, "\n")
	if width <= 0 || lipgloss.Width(full) <= width-2 {
		return full
	}
	compactLines := []string{
		tuiBannerClawStyle.Render("██████╗██╗      █████╗ ██╗    ██╗") + " " + tuiBannerGoStyle.Render(" ██████╗  ██████╗ "),
		tuiBannerClawStyle.Render("██╔═══╝██║     ██╔══██╗██║ █╗ ██║") + " " + tuiBannerGoStyle.Render("██╔════╝ ██╔═══██╗"),
		tuiBannerClawStyle.Render("██║    ██║     ███████║██║███╗██║") + " " + tuiBannerGoStyle.Render("██║  ███╗██║   ██║"),
		tuiBannerClawStyle.Render("╚██████╗███████╗██║  ██║╚███╔███╔╝") + " " + tuiBannerGoStyle.Render("╚██████╔╝╚██████╔╝"),
	}
	compact := strings.Join(compactLines, "\n")
	if lipgloss.Width(compact) <= width-2 {
		return compact
	}
	short := tuiBannerClawStyle.Render("Claw") + tuiBannerGoStyle.Render("Go")
	if lipgloss.Width(short) <= width-2 {
		return short
	}
	return tuiTitleStyle.Render("ClawGo")
}

func compactGatewayLabel(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.TrimPrefix(text, "http://")
	text = strings.TrimPrefix(text, "https://")
	return text
}

func intersperse(items []string, sep string) []string {
	if len(items) <= 1 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, item)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func filterTUISessions(items []tuiSession) []tuiSession {
	out := make([]tuiSession, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tuiSession{Key: key})
	}
	return out
}

package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AbandonwareDev/umbra/internal/config"
	"github.com/AbandonwareDev/umbra/internal/ipc"
	"github.com/AbandonwareDev/umbra/internal/types"
)

// ── Colors ──

var (
	titleFg  = lipgloss.Color("#FFF")
	titleBg  = lipgloss.Color("#7D56F4")
	accent   = lipgloss.Color("#7D56F4")
	green    = lipgloss.Color("#04B575")
	red      = lipgloss.Color("#FF6B6B")
	errRed   = lipgloss.Color("#FF4444")
	subtle   = lipgloss.Color("#626262")
	dark     = lipgloss.Color("#4A4A4A")
	light    = lipgloss.Color("#A7A7A7")
	onAccent = lipgloss.Color("#D5C8FF")
)

// ── Styles ──

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(titleFg).Background(titleBg).Padding(0, 2)
	titleBarStyle   = lipgloss.NewStyle().Background(titleBg)
	titleRightStyle = lipgloss.NewStyle().Foreground(onAccent).Background(titleBg)

	runningDot = lipgloss.NewStyle().Foreground(green).SetString("●")
	stoppedDot = lipgloss.NewStyle().Foreground(red).SetString("●")
	errorDot   = lipgloss.NewStyle().Foreground(errRed).SetString("●")
	unknownDot = lipgloss.NewStyle().Foreground(dark).SetString("●")

	runningStyle = lipgloss.NewStyle().Foreground(green)
	stoppedStyle = lipgloss.NewStyle().Foreground(red)
	errorStyle   = lipgloss.NewStyle().Foreground(errRed)
	dimStyle     = lipgloss.NewStyle().Foreground(dark)
	subtleStyle  = lipgloss.NewStyle().Foreground(subtle)
	itemStyle    = lipgloss.NewStyle().PaddingLeft(2)

	selStyle = lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(titleFg).
			Background(accent)

	errorBoxStyle = lipgloss.NewStyle().
			Background(errRed).
			Foreground(titleFg).
			Padding(0, 1)

	spinnerStyle = lipgloss.NewStyle().Foreground(accent)

	badgeStyle = lipgloss.NewStyle().
			Foreground(dark).
			Background(lipgloss.Color("#2A2A2A")).
			Padding(0, 1)
)

// ── Key bindings ──

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Toggle  key.Binding
	Refresh key.Binding
	Filter  key.Binding
	Quit    key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Refresh, k.Filter, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Toggle, k.Refresh, k.Filter, k.Quit}}
}

// ── Extension labels ──

func typeLabel(ext string) string {
	labels := map[string]string{
		".ovpn":         "OpenVPN",
		".wg":           "WireGuard",
		".conf":         "WireGuard",
		".torrc":        "Tor",
		".sgb":          "sing-box",
		".json":         "sing-box",
		".systemd":      "systemd",
		".systemd-user": "systemd-user",
	}
	if l, ok := labels[ext]; ok {
		return l
	}
	return "Custom"
}

// ── VPN Item ──

type VPNItem struct {
	name      string
	extension string
	status    types.VPNStatus
	pid       int
	errorMsg  string
}

func (i VPNItem) Title() string       { return i.name }
func (i VPNItem) Description() string { return "" }
func (i VPNItem) FilterValue() string { return i.name }

// ── Custom List Delegate ──

type vpnDelegate struct{}

func (d vpnDelegate) Height() int                             { return 2 }
func (d vpnDelegate) Spacing() int                            { return 0 }
func (d vpnDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d vpnDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(VPNItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()

	var statusText string
	switch item.status {
	case types.StatusRunning:
		statusText = fmt.Sprintf("● Running (PID: %d)", item.pid)
	case types.StatusStopped:
		statusText = "○ Stopped"
	case types.StatusError:
		statusText = fmt.Sprintf("✗ %s", item.errorMsg)
	default:
		statusText = "○ Unknown"
	}

	if isSelected {
		var statusFg lipgloss.Color
		switch item.status {
		case types.StatusRunning:
			statusFg = green
		case types.StatusStopped:
			statusFg = red
		case types.StatusError:
			statusFg = errRed
		default:
			statusFg = dark
		}

		// Line 1: name left, status right
		nameStr := fmt.Sprintf("▸ %s", item.name)
		namePart := lipgloss.NewStyle().Foreground(titleFg).Background(accent).Render(nameStr)
		statusPart := lipgloss.NewStyle().Foreground(statusFg).Background(accent).Render(fmt.Sprintf(" %s ", statusText))

		pad := lipgloss.NewStyle().Background(accent).Render("  ")
		avail := m.Width() - lipgloss.Width(pad) - lipgloss.Width(namePart) - lipgloss.Width(statusPart)
		if avail < 1 {
			avail = 1
		}
		spacer := lipgloss.NewStyle().Background(accent).Render(strings.Repeat(" ", avail))
		fmt.Fprintln(w, pad+namePart+spacer+statusPart)

		// Line 2: extension badge + type, left-aligned
		badge := badgeStyle.Render(item.extension)
		typeInfo := subtleStyle.Render(typeLabel(item.extension))
		fmt.Fprintln(w, lipgloss.NewStyle().PaddingLeft(2).Foreground(dark).Width(m.Width()).Render(fmt.Sprintf("%s %s", badge, typeInfo)))
		return
	}

	var statusFg lipgloss.Color
	switch item.status {
	case types.StatusRunning:
		statusFg = green
	case types.StatusStopped:
		statusFg = red
	case types.StatusError:
		statusFg = errRed
	default:
		statusFg = dark
	}

	// Line 1: name left (neutral), status right (colored)
	namePart := lipgloss.NewStyle().Foreground(subtle).Render(item.name)
	statusPart := lipgloss.NewStyle().Foreground(statusFg).Render(fmt.Sprintf(" %s ", statusText))

	avail := m.Width() - 2 - lipgloss.Width(namePart) - lipgloss.Width(statusPart)
	if avail < 1 {
		avail = 1
	}
	fmt.Fprintln(w, lipgloss.NewStyle().PaddingLeft(2).Render(namePart+strings.Repeat(" ", avail)+statusPart))

	// Line 2: extension badge + type, left-aligned
	badge := badgeStyle.Render(item.extension)
	typeInfo := subtleStyle.Render(typeLabel(item.extension))
	fmt.Fprintln(w, lipgloss.NewStyle().PaddingLeft(2).Foreground(dark).Width(m.Width()).Render(fmt.Sprintf("%s %s", badge, typeInfo)))
}

// ── Messages ──

type (
	configsLoadedMsg struct {
		configs []types.VPNConfig
	}
	configToggledMsg struct {
		name   string
		status types.VPNStatus
		err    error
	}
	errorMsg      struct{ err error }
	connectedMsg  struct{ socketPath string }
	daemonDeadMsg struct{}
)

// ── Model ──

type Model struct {
	paths      *config.AppPaths
	configs    []types.VPNConfig
	list       list.Model
	spinner    spinner.Model
	helpModel  help.Model
	keys       keyMap
	loading    bool
	err        error
	connected  bool
	socketPath string // actual daemon socket path (user or root mode)
	statusMsg  string
	width      int
	height     int
}

func NewModel(paths *config.AppPaths) Model {
	items := []list.Item{}
	deck := list.New(items, vpnDelegate{}, 0, 0)
	deck.SetShowTitle(false)
	deck.SetShowStatusBar(false)
	deck.SetShowHelp(false)
	deck.SetFilteringEnabled(true)
	deck.DisableQuitKeybindings()

	s := spinner.New()
	s.Style = spinnerStyle
	s.Spinner = spinner.Dot

	h := help.New()
	h.ShowAll = false

	km := keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Toggle:  key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "toggle")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}

	return Model{
		paths:     paths,
		list:      deck,
		spinner:   s,
		helpModel: h,
		keys:      km,
		loading:   true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		connectToDaemon(m.paths.SocketPath),
	)
}

// ── Dynamic overhead ──

func (m Model) calcListHeight() int {
	if m.width == 0 || m.height == 0 {
		return 0
	}
	lines := 1 // title bar
	lines += 2 // blank lines before list
	if m.loading && m.connected {
		lines += 2 // spinner line + trailing blank
	}
	if !m.connected && m.err != nil {
		lines += 2 // error banner + blank
	}
	if !m.connected && m.err == nil {
		lines += 2 // disconnected hint + blank
	}
	lines += 3 // blank before help + help bar + trailing newline
	lines += 2 // list internal frame/pagination space
	listH := m.height - lines
	if listH < 2 {
		listH = 2
	}
	return listH
}

// ── Update ──

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height - 3
		m.helpModel.Width = msg.Width

		listW := m.width * 60 / 100
		if listW < 40 {
			listW = 40
		}
		if listW > msg.Width-4 {
			listW = msg.Width - 4
		}
		m.list.SetWidth(listW)
		m.list.SetShowPagination(m.height > 24)

		m.list.SetHeight(m.calcListHeight())
		return m, nil

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.list.SetHeight(m.calcListHeight())
			return m, fetchConfigs(m.socketPath)
		case "enter", " ":
			selected, ok := m.list.SelectedItem().(VPNItem)
			if !ok {
				return m, nil
			}
			m.loading = true
			m.list.SetHeight(m.calcListHeight())
			return m, toggleVPN(m.socketPath, selected.name, selected.status)
		}

	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		if msg.Button != tea.MouseButtonLeft && msg.Button != tea.MouseButtonRight {
			return m, nil
		}
		if !m.connected || len(m.configs) == 0 {
			return m, nil
		}

		// Calculate list Y position in terminal
		listY := 1 // title bar
		// listY += 4  // blank lines before list
		if m.loading && m.connected {
			listY += 2 // spinner
		}

		// Check if click is within list bounds
		if msg.Y < listY || msg.Y >= listY+m.list.Height() {
			return m, nil
		}

		// Calculate visible item index from Y position.
		// Each item: delegate.Height()=2 content lines + 1 spacing line.
		const itemVHeight = 3
		relY := msg.Y - listY
		visIdx := relY / itemVHeight
		if visIdx >= m.list.Paginator.PerPage {
			return m, nil
		}

		globalIdx := m.list.Paginator.Page*m.list.Paginator.PerPage + visIdx
		totalItems := len(m.list.VisibleItems())
		if globalIdx >= totalItems {
			globalIdx = totalItems - 1
		}
		if globalIdx < 0 {
			return m, nil
		}
		m.list.Select(globalIdx)

		// Right click toggles the selected config.
		if msg.Button == tea.MouseButtonRight {
			selected, ok := m.list.SelectedItem().(VPNItem)
			if !ok {
				return m, nil
			}
			m.loading = true
			m.list.SetHeight(m.calcListHeight())
			return m, toggleVPN(m.socketPath, selected.name, selected.status)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case connectedMsg:
		m.connected = true
		m.loading = false
		m.socketPath = msg.socketPath
		m.list.SetHeight(m.calcListHeight())
		return m, fetchConfigs(m.socketPath)

	case daemonDeadMsg:
		m.connected = false
		m.loading = false
		m.err = fmt.Errorf("cannot connect to daemon — is it running?")
		m.configs = nil
		m.list.SetItems([]list.Item{})
		m.list.SetHeight(m.calcListHeight())
		return m, nil

	case configsLoadedMsg:
		m.loading = false
		m.err = nil
		m.configs = msg.configs
		items := make([]list.Item, len(msg.configs))
		for i, c := range msg.configs {
			items[i] = VPNItem{
				name:      c.Name,
				extension: c.Extension,
				status:    c.Status,
				pid:       c.PID,
				errorMsg:  c.ErrorMsg,
			}
		}
		m.list.SetItems(items)
		m.list.SetHeight(m.calcListHeight())
		return m, nil

	case configToggledMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.statusMsg = fmt.Sprintf("Failed to toggle %s: %s", msg.name, msg.err)
		} else {
			m.err = nil
			m.statusMsg = fmt.Sprintf("%s: %s", msg.name, msg.status)
		}
		m.list.SetHeight(m.calcListHeight())
		return m, fetchConfigs(m.socketPath)

	case errorMsg:
		m.loading = false
		m.err = msg.err
		m.list.SetHeight(m.calcListHeight())
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// ── View ──

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder
	running := 0
	for _, c := range m.configs {
		if c.Status == types.StatusRunning {
			running++
		}
	}

	// ── Title bar ──
	const titleOffset = 2
	titleLeft := titleStyle.Render(" Umbra ")
	var titleRight string
	if m.connected {
		titleRight = titleRightStyle.Render(fmt.Sprintf("  ● connected  │  %d/%d running  ", running, len(m.configs)))
	} else {
		titleRight = titleRightStyle.Render("  ○ disconnected  ")
	}
	fill := m.width - titleOffset - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if fill < 0 {
		fill = 0
	}

	b.WriteString(titleBarStyle.Render(strings.Repeat(" ", titleOffset)))
	b.WriteString(titleLeft)
	b.WriteString(titleBarStyle.Render(strings.Repeat(" ", fill)))
	b.WriteString(titleRight)
	// b.WriteString("\n")

	// ── Error banner ──
	if m.err != nil && !m.connected {
		b.WriteString(strings.Repeat(" ", 2))
		b.WriteString(errorBoxStyle.Render(fmt.Sprintf(" ⚠ %s ", m.err)))
		b.WriteString("\n\n")
	}

	// ── Centered list ──
	listW := m.list.Width()
	leftPad := (m.width - listW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	listOff := leftPad + titleOffset
	if listOff+listW > m.width {
		listOff = m.width - listW
	}
	if listOff < 0 {
		listOff = 0
	}

	b.WriteString("\n")
	// b.WriteString("\n")
	// b.WriteString("\n")
	// b.WriteString("\n")

	// Loading spinner
	if m.loading && m.connected {
		b.WriteString(strings.Repeat(" ", listOff))
		b.WriteString(fmt.Sprintf("  %s Loading...\n\n", m.spinner.View()))
	}

	// Disconnected hint
	if !m.connected && m.err == nil {
		b.WriteString(strings.Repeat(" ", listOff))
		b.WriteString(dimStyle.Render("  Waiting for daemon..."))
		b.WriteString("\n\n")
	}

	// VPN list
	if m.connected {
		if len(m.configs) > 0 {
			listView := m.list.View()
			b.WriteString(lipgloss.NewStyle().PaddingLeft(listOff).Render(listView))
		} else if !m.loading && m.err == nil {
			b.WriteString(strings.Repeat(" ", listOff))
			b.WriteString(dimStyle.Render("  No VPN config files found."))
			b.WriteString("\n")
		}
	}

	// ── Help bar (centered) ──
	b.WriteString("\n")
	m.helpModel.Width = m.width
	helpView := m.helpModel.View(m.keys)
	helpView = lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(helpView)
	b.WriteString(helpView)
	b.WriteString("\n")

	// Fill remaining vertical space so the TUI always uses the full terminal.
	output := b.String()
	visible := strings.Count(output, "\n")
	for visible < m.height {
		output += "\n"
		visible++
	}

	return output
}

// ── Commands ──

func connectToDaemon(socketPath string) tea.Cmd {
	return func() tea.Msg {
		// Try the configured socket path first (user mode: /tmp/umbra-$UID/daemon.sock)
		_, err := ipc.SendRequest(socketPath, &ipc.Request{Action: ipc.ActionList})
		if err == nil {
			return connectedMsg{socketPath: socketPath}
		}
		// Fall back to the root-mode socket path (/tmp/umbra/daemon.sock)
		if socketPath != config.RootSocketPath {
			_, err := ipc.SendRequest(config.RootSocketPath, &ipc.Request{Action: ipc.ActionList})
			if err == nil {
				return connectedMsg{socketPath: config.RootSocketPath}
			}
		}
		return daemonDeadMsg{}
	}
}

func fetchConfigs(socketPath string) tea.Cmd {
	return func() tea.Msg {
		resp, err := ipc.SendRequest(socketPath, &ipc.Request{Action: ipc.ActionList})
		if err != nil {
			return errorMsg{err: fmt.Errorf("fetching configs: %w", err)}
		}
		if !resp.Success {
			return errorMsg{err: fmt.Errorf("daemon error: %s", resp.Error)}
		}
		return configsLoadedMsg{configs: resp.Configs}
	}
}

func toggleVPN(socketPath, name string, currentStatus types.VPNStatus) tea.Cmd {
	return func() tea.Msg {
		action := ipc.ActionStart
		if currentStatus == types.StatusRunning {
			action = ipc.ActionStop
		}
		resp, err := ipc.SendRequest(socketPath, &ipc.Request{Action: action, Config: name})
		if err != nil {
			return configToggledMsg{name: name, err: fmt.Errorf("communication error: %w", err)}
		}
		if !resp.Success {
			return configToggledMsg{name: name, err: fmt.Errorf("daemon error: %s", resp.Error)}
		}
		var newStatus types.VPNStatus
		if len(resp.Configs) > 0 {
			newStatus = resp.Configs[0].Status
		}
		return configToggledMsg{name: name, status: newStatus}
	}
}

// ── Entry Point ──

func Run(paths *config.AppPaths) error {
	p := tea.NewProgram(NewModel(paths), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %s\n", err)
		os.Exit(1)
	}
	return nil
}

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KontangoOSS/schmutz/internal/join"
)

// ── Styles ──────────────────────────────────────────────────────────────────

var (
	colorPrimary = lipgloss.Color("#4a86c8")
	colorGreen   = lipgloss.Color("#38a169")
	colorOrange  = lipgloss.Color("#f58220")
	colorMuted   = lipgloss.Color("#6b7280")
	colorError   = lipgloss.Color("#e53e3e")
	colorBg      = lipgloss.Color("#1a1a2e")

	styleBanner = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(18)

	styleValue = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e2e8f0"))

	styleDim = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	styleError = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	styleSelected = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleStep = lipgloss.NewStyle().
			Foreground(colorOrange)

	styleSectionTitle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(colorPrimary).
				Width(54)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 2).
			Width(58)

	styleProgressDone = lipgloss.NewStyle().Foreground(colorGreen)
	styleProgressFail = lipgloss.NewStyle().Foreground(colorError)
	styleProgressWait = lipgloss.NewStyle().Foreground(colorMuted)
)

// ── Wizard steps ─────────────────────────────────────────────────────────────

type wizardStep int

const (
	stepURL      wizardStep = iota // text input for controller URL
	stepMethod                     // choose new / approle / invite
	stepCreds                      // enter credentials for chosen method
	stepScanning                   // fingerprint collection in progress
	stepConfirm                    // review collected data + optional profile
	stepProgress                   // live SSE enrollment stream
	stepDone                       // success or failure
)

type enrollMethod int

const (
	methodNew     enrollMethod = iota // anonymous — lands in quarantine
	methodApprole                     // Bao AppRole (trusted)
	methodInvite                      // one-time invite token
)

// ── Messages ─────────────────────────────────────────────────────────────────

type fingerprintDoneMsg struct {
	fp  *join.Fingerprint
	err error
}

type profileListMsg struct {
	profiles []string
	err      error
}

type progressLineMsg struct {
	icon string // ✓ ✗ →
	text string
	done bool // enrollment complete
	err  error
}

type enrollDoneMsg struct {
	result *joinResult
	lines  []progressLine // SSE events collected during enrollment
	err    error
}

// ── Model ─────────────────────────────────────────────────────────────────────

type joinModel struct {
	step    wizardStep
	width   int
	height  int
	spinner spinner.Model

	// Step: URL
	urlInput textinput.Model

	// Step: method
	methodCursor int
	methods      []string

	// Step: creds
	credInputs []textinput.Model
	credFocus  int

	// Step: scanning
	fp *join.Fingerprint

	// Step: confirm
	profiles      []string
	profileCursor int
	showProfiles  bool

	// Step: progress
	progressLines []progressLine
	enrollResult  *joinResult

	// Pre-filled from CLI flags
	prefillURL      string
	prefillSession  string
	prefillRoleID   string
	prefillSecretID string

	err error
}

type progressLine struct {
	icon string
	text string
}

// ── Init ──────────────────────────────────────────────────────────────────────

func newJoinModel(prefillURL, prefillSession, prefillRoleID, prefillSecretID string) joinModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorOrange)

	urlIn := textinput.New()
	urlIn.Placeholder = "https://your-controller.example"
	urlIn.CharLimit = 256
	if prefillURL != "" {
		urlIn.SetValue(prefillURL)
	}

	m := joinModel{
		spinner:         sp,
		urlInput:        urlIn,
		methods:         []string{"new — anonymous (quarantine)", "approle — trusted (Bao AppRole)", "invite — one-time token"},
		prefillURL:      prefillURL,
		prefillSession:  prefillSession,
		prefillRoleID:   prefillRoleID,
		prefillSecretID: prefillSecretID,
	}

	// If URL was pre-filled, skip to method step
	if prefillURL != "" {
		m.step = stepMethod
	} else {
		m.urlInput.Focus()
		m.step = stepURL
	}

	// Pre-select method based on flags
	if prefillRoleID != "" || prefillSecretID != "" {
		m.methodCursor = int(methodApprole)
	} else if prefillSession != "" {
		m.methodCursor = int(methodInvite)
	}

	return m
}

func (m joinModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m joinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case fingerprintDoneMsg:
		if msg.err != nil {
			m.err = msg.err
			m.step = stepDone
			return m, nil
		}
		m.fp = msg.fp
		// Fetch profiles in background while showing confirm screen
		m.step = stepConfirm
		return m, fetchProfiles(m.urlInput.Value())

	case profileListMsg:
		if msg.err == nil && len(msg.profiles) > 0 {
			m.profiles = msg.profiles
			m.showProfiles = true
		}
		return m, nil

	case progressLineMsg:
		if msg.err != nil {
			m.progressLines = append(m.progressLines, progressLine{"✗", msg.err.Error()})
			m.step = stepDone
			m.err = msg.err
			return m, nil
		}
		m.progressLines = append(m.progressLines, progressLine{msg.icon, msg.text})
		if msg.done {
			m.step = stepDone
		}
		return m, nil

	case enrollDoneMsg:
		// Show the collected SSE lines in the progress view before done.
		if len(msg.lines) > 0 {
			m.progressLines = msg.lines
		}
		if msg.err != nil {
			m.err = msg.err
			m.step = stepDone
			return m, nil
		}
		m.enrollResult = msg.result
		m.step = stepDone
		return m, nil
	}

	return m, nil
}

func (m joinModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.step {

	case stepURL:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			v := strings.TrimSpace(m.urlInput.Value())
			if v == "" {
				return m, nil
			}
			if !strings.HasPrefix(v, "http") {
				v = "https://" + v
				m.urlInput.SetValue(v)
			}
			m.step = stepMethod
			return m, nil
		default:
			var cmd tea.Cmd
			m.urlInput, cmd = m.urlInput.Update(msg)
			return m, cmd
		}

	case stepMethod:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.methodCursor > 0 {
				m.methodCursor--
			}
		case "down", "j":
			if m.methodCursor < len(m.methods)-1 {
				m.methodCursor++
			}
		case "enter":
			return m.enterMethod()
		}

	case stepCreds:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.step = stepMethod
			return m, nil
		case "tab", "down":
			m.credInputs[m.credFocus].Blur()
			m.credFocus = (m.credFocus + 1) % len(m.credInputs)
			m.credInputs[m.credFocus].Focus()
		case "shift+tab", "up":
			m.credInputs[m.credFocus].Blur()
			m.credFocus = (m.credFocus - 1 + len(m.credInputs)) % len(m.credInputs)
			m.credInputs[m.credFocus].Focus()
		case "enter":
			if m.credFocus < len(m.credInputs)-1 {
				m.credInputs[m.credFocus].Blur()
				m.credFocus++
				m.credInputs[m.credFocus].Focus()
			} else {
				return m.startScanning()
			}
		default:
			var cmd tea.Cmd
			m.credInputs[m.credFocus], cmd = m.credInputs[m.credFocus].Update(msg)
			return m, cmd
		}

	case stepConfirm:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.showProfiles && m.profileCursor > 0 {
				m.profileCursor--
			}
		case "down", "j":
			if m.showProfiles && m.profileCursor < len(m.profiles)-1 {
				m.profileCursor++
			}
		case "enter", "y":
			return m.startEnroll()
		case "n", "q":
			return m, tea.Quit
		}

	case stepDone:
		switch msg.String() {
		case "ctrl+c", "q", "esc", "enter":
			return m, tea.Quit
		}
	}

	return m, nil
}

// enterMethod sets up the creds step based on chosen method, or skips to scan.
func (m joinModel) enterMethod() (tea.Model, tea.Cmd) {
	switch enrollMethod(m.methodCursor) {

	case methodNew:
		// No credentials needed — go straight to fingerprint scan
		return m.startScanning()

	case methodApprole:
		ri := textinput.New()
		ri.Placeholder = "role_id"
		ri.CharLimit = 128
		if m.prefillRoleID != "" {
			ri.SetValue(m.prefillRoleID)
		}
		ri.Focus()

		si := textinput.New()
		si.Placeholder = "secret_id"
		si.CharLimit = 128
		si.EchoMode = textinput.EchoPassword
		si.EchoCharacter = '•'
		if m.prefillSecretID != "" {
			si.SetValue(m.prefillSecretID)
		}

		m.credInputs = []textinput.Model{ri, si}
		m.credFocus = 0
		m.step = stepCreds
		return m, textinput.Blink

	case methodInvite:
		ti := textinput.New()
		ti.Placeholder = "invite token"
		ti.CharLimit = 256
		if m.prefillSession != "" {
			ti.SetValue(m.prefillSession)
		}
		ti.Focus()

		m.credInputs = []textinput.Model{ti}
		m.credFocus = 0
		m.step = stepCreds
		return m, textinput.Blink
	}

	return m, nil
}

// startScanning kicks off fingerprint collection in the background.
func (m joinModel) startScanning() (tea.Model, tea.Cmd) {
	m.step = stepScanning
	return m, collectFingerprint()
}

// startEnroll begins the SSE enrollment stream.
func (m joinModel) startEnroll() (tea.Model, tea.Cmd) {
	m.step = stepProgress
	m.progressLines = nil

	url := m.urlInput.Value()
	method := enrollMethod(m.methodCursor)

	var session, roleID, secretID string
	switch method {
	case methodApprole:
		roleID = m.credInputs[0].Value()
		secretID = m.credInputs[1].Value()
	case methodInvite:
		session = m.credInputs[0].Value()
	}

	// If the chosen profile is not index 0 (which is "none"), include it.
	var profile string
	if m.showProfiles && m.profileCursor > 0 {
		profile = m.profiles[m.profileCursor]
	}

	return m, runEnrollStream(url, session, roleID, secretID, profile)
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m joinModel) View() string {
	var b strings.Builder

	b.WriteString(banner())
	b.WriteString("\n")

	switch m.step {
	case stepURL:
		b.WriteString(m.viewURL())
	case stepMethod:
		b.WriteString(m.viewMethod())
	case stepCreds:
		b.WriteString(m.viewCreds())
	case stepScanning:
		b.WriteString(m.viewScanning())
	case stepConfirm:
		b.WriteString(m.viewConfirm())
	case stepProgress:
		b.WriteString(m.viewProgress())
	case stepDone:
		b.WriteString(m.viewDone())
	}

	return b.String()
}

func banner() string {
	return styleBanner.Render("schmutz") +
		styleDim.Render("  /ʃmʊts/  ·  zero-trust enrollment") + "\n"
}

func (m joinModel) viewURL() string {
	var b strings.Builder
	b.WriteString(styleSectionTitle.Render("Controller URL") + "\n\n")
	b.WriteString(styleDim.Render("Where is your Schmutz controller?") + "\n\n")
	b.WriteString(m.urlInput.View() + "\n\n")
	b.WriteString(styleDim.Render("enter to continue · esc to quit") + "\n")
	return styleBox.Render(b.String())
}

func (m joinModel) viewMethod() string {
	var b strings.Builder
	b.WriteString(styleSectionTitle.Render("Enrollment method") + "\n\n")
	b.WriteString(styleLabel.Render("Controller:") + styleValue.Render(m.urlInput.Value()) + "\n\n")

	for i, choice := range m.methods {
		cursor := "  "
		style := styleDim
		if i == m.methodCursor {
			cursor = "▶ "
			style = styleSelected
		}
		b.WriteString(style.Render(cursor+choice) + "\n")
	}

	b.WriteString("\n" + styleDim.Render("↑↓ or j/k to move · enter to select · esc to quit") + "\n")
	return styleBox.Render(b.String())
}

func (m joinModel) viewCreds() string {
	var b strings.Builder
	method := enrollMethod(m.methodCursor)

	switch method {
	case methodApprole:
		b.WriteString(styleSectionTitle.Render("Bao AppRole credentials") + "\n\n")
		b.WriteString(styleDim.Render("Trusted enrollment — skips quarantine.") + "\n\n")
		b.WriteString(styleLabel.Render("Role ID:") + "\n" + m.credInputs[0].View() + "\n\n")
		b.WriteString(styleLabel.Render("Secret ID:") + "\n" + m.credInputs[1].View() + "\n\n")
	case methodInvite:
		b.WriteString(styleSectionTitle.Render("Invite token") + "\n\n")
		b.WriteString(styleDim.Render("One-time token from your administrator.") + "\n\n")
		b.WriteString(styleLabel.Render("Token:") + "\n" + m.credInputs[0].View() + "\n\n")
	}

	b.WriteString(styleDim.Render("tab/↓ next field · shift+tab/↑ prev · enter to continue · esc back") + "\n")
	return styleBox.Render(b.String())
}

func (m joinModel) viewScanning() string {
	var b strings.Builder
	b.WriteString(styleSectionTitle.Render("Collecting machine data") + "\n\n")
	b.WriteString(m.spinner.View() + styleDim.Render("  scanning hardware, network, system…") + "\n")
	return styleBox.Render(b.String())
}

func (m joinModel) viewConfirm() string {
	var b strings.Builder
	b.WriteString(styleSectionTitle.Render("Confirm enrollment") + "\n\n")

	if m.fp != nil {
		rows := [][]string{
			{"Hostname:", m.fp.Hostname},
			{"OS:", m.fp.OS + " / " + m.fp.Arch},
			{"Kernel:", m.fp.KernelVersion},
			{"CPU:", truncate(m.fp.CPUInfo, 36)},
			{"Machine ID:", truncate(m.fp.MachineID, 24)},
			{"Hardware hash:", m.fp.HardwareHash},
			{"MACs:", strings.Join(m.fp.MACAddrs, ", ")},
		}
		for _, row := range rows {
			b.WriteString(styleLabel.Render(row[0]) + styleValue.Render(row[1]) + "\n")
		}
	}

	if m.showProfiles {
		b.WriteString("\n" + styleSectionTitle.Render("Profile  (optional)") + "\n\n")
		allProfiles := append([]string{"— none —"}, m.profiles...)
		for i, p := range allProfiles {
			cursor := "  "
			style := styleDim
			if i == m.profileCursor {
				cursor = "▶ "
				style = styleSelected
			}
			b.WriteString(style.Render(cursor+p) + "\n")
		}
		b.WriteString("\n")
	} else if m.profiles == nil {
		// Still fetching
		b.WriteString("\n" + m.spinner.View() + styleDim.Render("  loading profiles…") + "\n\n")
	}

	b.WriteString(styleDim.Render("y/enter to enroll · n/esc to abort · ↑↓ to select profile") + "\n")
	return styleBox.Render(b.String())
}

func (m joinModel) viewProgress() string {
	var b strings.Builder
	b.WriteString(styleSectionTitle.Render("Enrolling") + "\n\n")

	for _, line := range m.progressLines {
		var icon string
		switch line.icon {
		case "✓":
			icon = styleProgressDone.Render("✓")
		case "✗":
			icon = styleProgressFail.Render("✗")
		default:
			icon = styleProgressWait.Render(line.icon)
		}
		b.WriteString(icon + "  " + styleValue.Render(line.text) + "\n")
	}

	if m.step == stepProgress {
		b.WriteString(m.spinner.View() + "\n")
	}

	return styleBox.Render(b.String())
}

func (m joinModel) viewDone() string {
	var b strings.Builder

	if m.err != nil {
		b.WriteString(styleError.Render("✗ enrollment failed") + "\n\n")
		b.WriteString(styleValue.Render(m.err.Error()) + "\n\n")
		b.WriteString(styleDim.Render("press enter or q to exit") + "\n")
	} else if m.enrollResult != nil {
		r := m.enrollResult
		nick := r.Nickname
		if nick == "" {
			nick = r.ID[:8]
		}
		b.WriteString(styleSuccess.Render("✓ enrolled") + "\n\n")
		b.WriteString(styleLabel.Render("Nickname:") + styleValue.Render(nick) + "\n")
		b.WriteString(styleLabel.Render("ID:") + styleValue.Render(r.ID) + "\n")
		b.WriteString(styleLabel.Render("Status:") + styleValue.Render(r.Status) + "\n\n")

		b.WriteString(styleDim.Render("Tunnel is up. Agent and Caddy are installed.\n"))
		b.WriteString(styleDim.Render("This machine is now on the mesh.") + "\n\n")
		b.WriteString(styleDim.Render("press enter to exit") + "\n")
	} else {
		// Progress lines finished but no result yet — show them
		b.WriteString(m.viewProgress())
		b.WriteString("\n" + styleDim.Render("press enter to exit") + "\n")
	}

	return styleBox.Render(b.String())
}

// ── Commands ─────────────────────────────────────────────────────────────────

// collectFingerprint gathers system fingerprint in a goroutine.
func collectFingerprint() tea.Cmd {
	return func() tea.Msg {
		fp, err := join.Collect()
		return fingerprintDoneMsg{fp: fp, err: err}
	}
}

// fetchProfiles asks the controller for the list of available profiles.
func fetchProfiles(baseURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(baseURL + "/api/profiles")
		if err != nil {
			return profileListMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return profileListMsg{}
		}
		var list []string
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			return profileListMsg{}
		}
		return profileListMsg{profiles: list}
	}
}

// runEnrollStream runs the enrollment via SSEEnrollStream and collects all
// progress lines. Returns enrollDoneMsg with lines + result when complete.
func runEnrollStream(url, session, roleID, secretID, profile string) tea.Cmd {
	return func() tea.Msg {
		method := "new"
		if roleID != "" {
			method = "approle"
		} else if session != "" {
			method = "invite"
		}

		var lines []progressLine
		sseResult, err := join.SSEEnrollStream(url, method, session, roleID, secretID, func(ev join.SSEEvent) {
			switch ev.Kind {
			case "verify":
				icon := "✓"
				if !ev.Passed {
					icon = "✗"
				}
				label := ev.Check
				if ev.Reason != "" && !ev.Passed {
					label += ": " + ev.Reason
				}
				lines = append(lines, progressLine{icon, label})
			case "progress":
				lines = append(lines, progressLine{"→", ev.Step})
			case "decision":
				if ev.Status == "rejected" {
					lines = append(lines, progressLine{"✗", "rejected: " + ev.Reason})
				} else {
					lines = append(lines, progressLine{"✓", "accepted"})
				}
			case "identity":
				lines = append(lines, progressLine{"✓", "identity issued"})
			}
		})

		if err != nil {
			return enrollDoneMsg{err: err, lines: lines}
		}
		result := &joinResult{
			ID:       sseResult.ID,
			Nickname: sseResult.Nickname,
			Identity: sseResult.Identity,
			Status:   sseResult.Status,
			Hosts:    sseResult.Config.Hosts,
			Tunnel:   sseResult.Config.Tunnel,
		}
		return enrollDoneMsg{result: result, lines: lines}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// ── Entry point ───────────────────────────────────────────────────────────────

// runJoinTUI launches the interactive wizard and returns the joinResult when done.
// It takes pre-filled values from CLI flags; any non-empty value skips that step.
func runJoinTUI(prefillURL, prefillSession, prefillRoleID, prefillSecretID string) (*joinResult, error) {
	m := newJoinModel(prefillURL, prefillSession, prefillRoleID, prefillSecretID)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	fm := final.(joinModel)
	if fm.err != nil {
		return nil, fm.err
	}
	return fm.enrollResult, nil
}

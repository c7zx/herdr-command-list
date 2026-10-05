package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	pluginID     = "herdr.command-lists"
	version      = "1.0.2"
	targetEnvKey = "HERDR_COMMAND_LISTS_TARGET_PANE"
)

type pluginContext struct {
	FocusedPaneID string `json:"focused_pane_id"`
}

type entryKind uint8

const (
	entryBlank entryKind = iota
	entryHeader
	entryCommand
)

type listEntry struct {
	kind entryKind
	text string
}

type uiModel struct {
	lists      []commandList
	activeTab  int
	entries    []listEntry
	visible    []int
	query      []rune
	selected   int // index into visible; -1 means no selection
	offset     int // first visible row; retained while selection stays on screen
	help       bool
	helpOffset int
	helpPath   string
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: command-lists <open|ui|init|help|version>")
	}
	// Internal lifecycle helper used by install.sh. Keep it out of user-facing
	// help: it only parses Herdr's plugin-list JSON from stdin.
	if len(os.Args) == 2 && os.Args[1] == "installed-root" {
		root, err := installedRootFromJSON(os.Stdin)
		if err != nil {
			fatal(err.Error())
		}
		fmt.Print(root)
		return
	}
	if os.Args[1] == "shortcut-config" && (len(os.Args) == 3 || len(os.Args) == 4) {
		path := herdrConfigPath()
		if len(os.Args) == 4 {
			path = os.Args[3]
		}
		status, err := configureShortcut(os.Args[2], path)
		if err != nil {
			fatal(err.Error())
		}
		fmt.Println(status)
		return
	}
	if len(os.Args) != 2 {
		fatal("expected one command; use command-lists help")
	}
	var err error
	switch os.Args[1] {
	case "open":
		err = openPopup()
	case "ui":
		err = runUI()
	case "init":
		var path string
		if path, err = ensureListsDir(); err == nil {
			fmt.Println(path)
		}
	case "help":
		printHelp()
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "command-lists:", safeDisplay(message))
	os.Exit(1)
}

func printHelp() {
	fmt.Println("Command Lists " + version)
	fmt.Println("  open     Open the popup through Herdr")
	fmt.Println("  ui       Run the popup UI (normally started by Herdr)")
	fmt.Println("  init     Initialize/verify lists and print the lists directory")
	fmt.Println("  help     Show this help")
	fmt.Println("  version  Show the plugin version")
}

// ---------- Herdr integration ----------

type pluginListResponse struct {
	Result struct {
		Plugins []struct {
			PluginID   string `json:"plugin_id"`
			PluginRoot string `json:"plugin_root"`
		} `json:"plugins"`
	} `json:"result"`
}

func installedRootFromJSON(r io.Reader) (string, error) {
	var response pluginListResponse
	if err := json.NewDecoder(r).Decode(&response); err != nil {
		return "", fmt.Errorf("parse Herdr plugin list: %w", err)
	}
	for _, plugin := range response.Result.Plugins {
		if plugin.PluginID == pluginID {
			return strings.TrimSpace(plugin.PluginRoot), nil
		}
	}
	return "", nil
}

func herdrBin() string {
	if p := strings.TrimSpace(os.Getenv("HERDR_BIN_PATH")); p != "" {
		return p
	}
	return "herdr"
}

func targetPane() string {
	if pane := strings.TrimSpace(os.Getenv(targetEnvKey)); pane != "" {
		return pane
	}
	if pane := strings.TrimSpace(os.Getenv("HERDR_ACTIVE_PANE_ID")); pane != "" {
		return pane
	}
	var ctx pluginContext
	if json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx) == nil {
		return strings.TrimSpace(ctx.FocusedPaneID)
	}
	return ""
}

func openPopup() error {
	target := targetPane()
	if target == "" {
		return errors.New("Herdr did not provide a focused target pane")
	}

	// Popup panes target the currently active pane automatically. Passing
	// --target-pane is invalid for popup placement, so only preserve the pane
	// ID in an explicit environment variable for command submission later.
	err := exec.Command(herdrBin(),
		"plugin", "pane", "open",
		"--plugin", pluginID,
		"--entrypoint", "command-lists",
		"--env", targetEnvKey+"="+target,
		"--focus",
	).Run()
	if err != nil {
		return fmt.Errorf("open popup: %w", err)
	}
	return nil
}

func pluginConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("HERDR_PLUGIN_CONFIG_DIR")); dir != "" {
		return dir, nil
	}
	out, err := exec.Command(herdrBin(), "plugin", "config-dir", pluginID).Output()
	if err != nil {
		return "", fmt.Errorf("get plugin config directory: %w", err)
	}
	if dir := strings.TrimSpace(string(out)); dir != "" {
		return dir, nil
	}
	return "", errors.New("Herdr returned an empty plugin config directory")
}

func herdrConfigPath() string {
	if p := strings.TrimSpace(os.Getenv("HERDR_CONFIG_PATH")); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "herdr", "config.toml")
	}
	return "~/.config/herdr/config.toml"
}

// ---------- small dependency-free TUI ----------

func runUI() error {
	dir, err := ensureListsDir()
	if err != nil {
		return err
	}
	lists, err := loadLists(dir)
	if err != nil {
		return err
	}
	target := targetPane()
	if target == "" {
		return errors.New("popup has no target pane; open it through the Command Lists action")
	}
	restore, err := rawTerminal()
	if err != nil {
		return err
	}
	defer restore()
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[?2004h")
	defer fmt.Print("\x1b[?2004l\x1b[?25h\x1b[?1049l")
	m := &uiModel{lists: lists, selected: -1,
		helpPath: filepath.Join(filepath.Dir(dir), helpFile)}
	if len(lists) > 0 {
		m.entries = lists[0].entries
	}
	m.refresh(false)
	for {
		rows, cols := terminalSize()
		draw(m, rows, cols)
		event, r, err := readKey()
		if err != nil {
			return err
		}
		if m.help {
			switch event {
			case "ctrl+c", "ctrl+q", "escape":
				return nil
			case "help":
				m.help = false
			case "up":
				m.helpOffset = max(0, m.helpOffset-1)
			case "down":
				m.helpOffset++
			}
			continue
		}
		switch event {
		case "ctrl+c", "ctrl+q", "escape":
			return nil
		case "help":
			m.help = true
		case "left":
			m.switchTab(-1)
		case "right":
			m.switchTab(1)
		case "ctrl+space":
			m.nextSection()
		case "up":
			m.up()
		case "down":
			m.down()
		case "backspace":
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.refresh(len(m.query) > 0)
			}
		case "ctrl+u":
			m.query = nil
			m.refresh(false)
		case "rune":
			if !unsafeRune(r) {
				m.query = append(m.query, r)
				m.refresh(true)
			}
		case "enter":
			if command, ok := m.current(); ok {
				return submitCommand(target, command)
			}
		}
	}
}

func (m *uiModel) refresh(selectFirst bool) {
	m.visible = filterEntries(m.entries, string(m.query))
	m.selected, m.offset = -1, 0
	if selectFirst {
		m.selected = m.firstSelectable()
	}
}

func (m *uiModel) switchTab(delta int) {
	if len(m.lists) == 0 {
		return
	}
	m.activeTab = (m.activeTab + delta%len(m.lists) + len(m.lists)) % len(m.lists)
	m.entries = m.lists[m.activeTab].entries
	m.query = nil
	m.refresh(false)
}

func (m *uiModel) ensureVisible(height int) {
	if height < 1 {
		m.offset = 0
		return
	}
	m.offset = min(max(m.offset, 0), max(len(m.visible)-height, 0))
	if m.selected < 0 || m.selected >= len(m.visible) {
		return
	}
	if m.selected < m.offset {
		m.offset = m.selected
	} else if m.selected >= m.offset+height {
		m.offset = m.selected - height + 1
	}
}

func filterEntries(entries []listEntry, query string) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		visible := make([]int, len(entries))
		for i := range entries {
			visible[i] = i
		}
		return visible
	}

	visible := make([]int, 0, len(entries))
	currentHeader := -1
	headerAdded := false
	for i, entry := range entries {
		switch entry.kind {
		case entryHeader:
			currentHeader = i
			headerAdded = false
		case entryCommand:
			if strings.Contains(strings.ToLower(entry.text), q) {
				if currentHeader >= 0 && !headerAdded {
					visible = append(visible, currentHeader)
					headerAdded = true
				}
				visible = append(visible, i)
			}
		}
	}
	return visible
}

func (m *uiModel) firstSelectable() int {
	for pos, entryIndex := range m.visible {
		if m.entries[entryIndex].kind == entryCommand {
			return pos
		}
	}
	return -1
}

func (m *uiModel) lastSelectable() int {
	for pos := len(m.visible) - 1; pos >= 0; pos-- {
		if m.entries[m.visible[pos]].kind == entryCommand {
			return pos
		}
	}
	return -1
}

func (m *uiModel) up() {
	if m.selected >= 0 {
		for pos := m.selected - 1; pos >= 0; pos-- {
			if m.entries[m.visible[pos]].kind == entryCommand {
				m.selected = pos
				return
			}
		}
	}
	m.selected = m.lastSelectable()
}

func (m *uiModel) down() {
	if m.selected >= 0 {
		for pos := m.selected + 1; pos < len(m.visible); pos++ {
			if m.entries[m.visible[pos]].kind == entryCommand {
				m.selected = pos
				return
			}
		}
	}
	m.selected = m.firstSelectable()
	m.offset = 0
}

func (m *uiModel) current() (string, bool) {
	if m.selected < 0 || m.selected >= len(m.visible) {
		return "", false
	}
	entry := m.entries[m.visible[m.selected]]
	if entry.kind != entryCommand {
		return "", false
	}
	return entry.text, true
}

func (m *uiModel) sectionStarts() []int {
	var starts []int
	waitingForCommand := false
	for i, entry := range m.entries {
		switch entry.kind {
		case entryHeader:
			waitingForCommand = true
		case entryCommand:
			if waitingForCommand {
				starts = append(starts, i)
				waitingForCommand = false
			}
		}
	}
	return starts
}

func (m *uiModel) selectEntry(entryIndex int) {
	for pos, idx := range m.visible {
		if idx == entryIndex {
			m.selected = pos
			return
		}
	}
	m.selected = -1
}

func (m *uiModel) nextSection() {
	currentEntry := -1
	if m.selected >= 0 && m.selected < len(m.visible) {
		currentEntry = m.visible[m.selected]
	}
	if len(m.query) > 0 {
		m.query = nil
		m.refresh(false)
		if currentEntry >= 0 {
			m.selectEntry(currentEntry)
		}
	}
	starts := m.sectionStarts()
	if len(starts) == 0 {
		return
	}
	if m.selected < 0 || m.selected >= len(m.visible) {
		m.selectEntry(starts[0])
		return
	}

	currentEntry = m.visible[m.selected]
	currentSection := -1
	for i, start := range starts {
		if start <= currentEntry {
			currentSection = i
		} else {
			break
		}
	}
	if currentSection < 0 {
		m.selectEntry(starts[0])
		return
	}
	m.selectEntry(starts[(currentSection+1)%len(starts)])
}

func countCommands(entries []listEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.kind == entryCommand {
			count++
		}
	}
	return count
}

func submitCommand(pane, command string) error {
	if err := validateCommand(command); err != nil {
		return err
	}
	if strings.TrimSpace(pane) == "" {
		return errors.New("missing target pane")
	}
	// Standard Emacs-style shell prompt: move to the end before killing the
	// line, so text on either side of the cursor is removed. Keep control keys
	// outside pane run: bracketed paste must not turn them into command text.
	if err := exec.Command(herdrBin(), "pane", "send-keys", pane, "ctrl+e", "ctrl+u").Run(); err != nil {
		return fmt.Errorf("clear target input: %w", err)
	}
	if err := exec.Command(herdrBin(), "pane", "run", pane, command).Run(); err != nil {
		return fmt.Errorf("run selected command: %w", err)
	}
	return nil
}

func draw(m *uiModel, rows, cols int) {
	fmt.Print(render(m, rows, cols))
}

func render(m *uiModel, rows, cols int) string {
	var b strings.Builder
	b.WriteString("\x1b[2J\x1b[H")
	if rows < 6 || cols < 12 {
		b.WriteString(truncate("Command Lists: enlarge popup", cols))
		return b.String()
	}
	if m.help {
		return renderHelp(m, rows, cols)
	}
	fmt.Fprintf(&b, "\x1b[1m%s\x1b[0m\r\n", truncate(
		fmt.Sprintf("Command Lists %s | %d commands", version, countCommands(m.entries)), cols))
	b.WriteString(renderTabs(m.lists, m.activeTab, cols))
	b.WriteString("\r\nSearch: ")
	if len(m.query) == 0 {
		fmt.Fprintf(&b, "\x1b[2m%s\x1b[0m", truncate("type to filter", cols-8))
	} else {
		b.WriteString(truncate(string(m.query), cols-8))
	}
	b.WriteString("\r\n\r\n")
	height := rows - 5
	m.ensureVisible(height)
	end := min(m.offset+height, len(m.visible))
	for pos := m.offset; pos < end; pos++ {
		entry := m.entries[m.visible[pos]]
		if entry.kind != entryBlank {
			prefix := "  "
			if pos == m.selected {
				prefix = "\x1b[7m> "
			}
			fmt.Fprintf(&b, "%s%s%s\x1b[0m", prefix, entryColor(entry), truncate(entry.text, cols-2))
		}
		b.WriteString("\r\n")
	}
	printed := end - m.offset
	if len(m.visible) == 0 {
		message := "No matching commands."
		if len(m.lists) == 0 {
			message = "No lists: add a .md file in lists/."
		}
		fmt.Fprintf(&b, "\x1b[2m%s\x1b[0m\r\n", truncate(message, cols))
		printed++
	}
	for i := printed; i < height; i++ {
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "\x1b[2m%s\x1b[0m", truncate("↑/↓ select | ←/→ lists | Enter run | ? help | Esc close", cols))
	return b.String()
}

func renderTabs(lists []commandList, active, cols int) string {
	if len(lists) == 0 {
		return truncate("[No lists]", cols)
	}
	active = min(max(active, 0), len(lists)-1)
	labels := make([]string, len(lists))
	for i, list := range lists {
		labels[i] = "[" + truncate(safeDisplay(list.name), min(14, max(cols-6, 1))) + "]"
	}
	start, end, used := active, active, displayWidth(labels[active])
	budget := max(cols-4, 0)
	for {
		grew := false
		if start > 0 && used+1+displayWidth(labels[start-1]) <= budget {
			start--
			used += 1 + displayWidth(labels[start])
			grew = true
		}
		if end+1 < len(lists) && used+1+displayWidth(labels[end+1]) <= budget {
			end++
			used += 1 + displayWidth(labels[end])
			grew = true
		}
		if !grew {
			break
		}
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString("‹ ")
	} else {
		b.WriteString("  ")
	}
	for i := start; i <= end; i++ {
		if i > start {
			b.WriteByte(' ')
		}
		if i == active {
			b.WriteString("\x1b[1;7m")
		}
		b.WriteString(labels[i])
		b.WriteString("\x1b[0m")
	}
	if end < len(lists)-1 {
		b.WriteString(" ›")
	}
	return b.String()
}

func entryColor(entry listEntry) string {
	depth := len(entry.text) - len(strings.TrimLeft(entry.text, " "))
	colors := []int{96, 92, 93, 95, 94, 97}
	if entry.kind == entryHeader {
		return fmt.Sprintf("\x1b[1;%dm", colors[depth%len(colors)])
	}
	if depth == 0 {
		return ""
	}
	return fmt.Sprintf("\x1b[%dm", colors[depth%len(colors)])
}

func renderHelp(m *uiModel, rows, cols int) string {
	lines, err := loadHelpLines(m.helpPath)
	if err != nil {
		lines = []string{"Unable to read HELP.md: " + safeDisplay(err.Error())}
	}
	height := rows - 2
	m.helpOffset = min(max(m.helpOffset, 0), max(len(lines)-height, 0))
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[2J\x1b[H\x1b[1m%s\x1b[0m\r\n", truncate("Command Lists Help", cols))
	for i := 0; i < height; i++ {
		if pos := m.helpOffset + i; pos < len(lines) {
			b.WriteString(truncate(safeDisplay(lines[pos]), cols))
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "\x1b[2m%s\x1b[0m", truncate("↑/↓ scroll | ? return | Esc/Ctrl+C close", cols))
	return b.String()
}

// Terminal-cell widths for combining characters and common wide Unicode
// ranges; no external rendering dependency is needed for short text labels.
func runeWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	n := 0
	for i, r := range s {
		if n+runeWidth(r) > width-1 {
			return s[:i] + "…"
		}
		n += runeWidth(r)
	}
	return s
}

// ---------- Unix terminal helpers (Linux + macOS) ----------

func rawTerminal() (func(), error) {
	get := exec.Command("stty", "-g")
	get.Stdin = os.Stdin
	state, err := get.Output()
	if err != nil {
		return nil, fmt.Errorf("stty -g: %w", err)
	}
	set := exec.Command("stty", "raw", "-echo", "min", "0", "time", "1")
	set.Stdin = os.Stdin
	if err := set.Run(); err != nil {
		return nil, fmt.Errorf("enable raw terminal: %w", err)
	}
	return func() {
		cmd := exec.Command("stty", strings.TrimSpace(string(state)))
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
	}, nil
}

func terminalSize() (rows, cols int) {
	rows, cols = 24, 80
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	if out, err := cmd.Output(); err == nil {
		var r, c int
		if _, err := fmt.Sscan(string(out), &r, &c); err == nil && r > 0 && c > 0 {
			rows, cols = r, c
		}
	}
	return
}

func readByte() (byte, bool, error) {
	var b [1]byte
	n, err := os.Stdin.Read(b[:])
	if errors.Is(err, io.EOF) || n == 0 {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return b[0], true, nil
}

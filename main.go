package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	pluginID     = "herdr.command-list"
	commandFile  = "commands.md"
	helpFile     = "HELP.md"
	targetEnvKey = "HERDR_COMMAND_LIST_TARGET_PANE"
)

//go:embed HELP.md
var embeddedHelp []byte

const starterCommands = `# command-list plugin
nano "$(herdr plugin config-dir herdr.command-list)/commands.md"
nano ~/.config/herdr/config.toml
cat "$(herdr plugin config-dir herdr.command-list)/HELP.md"

# example commands
whoami
date

# more examples
printf 'Hello World!\n'
`

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
	entries     []listEntry
	visible     []int
	query       []rune
	selected    int // index into visible; -1 means no selection
	help        bool
	commandPath string
	helpPath    string
	configPath  string
}

func main() {
	if len(os.Args) != 2 {
		fatal("usage: command-list <open|ui|init|help>")
	}

	var err error
	switch os.Args[1] {
	case "open":
		err = openPopup()
	case "ui":
		err = runUI()
	case "init":
		var path string
		if path, err = ensureCommandPath(); err == nil {
			fmt.Println(path)
		}
	case "help":
		printHelp()
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "command-list:", message)
	os.Exit(1)
}

func printHelp() {
	fmt.Println("Command List")
	fmt.Println("  open  Open the popup through Herdr")
	fmt.Println("  ui    Run the popup UI (normally started by Herdr)")
	fmt.Println("  init  Create/verify commands.md and print its path")
	fmt.Println("  help  Show this help")
}

// ---------- Herdr integration ----------

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
		"--entrypoint", "command-list",
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

// ---------- commands.md ----------

func ensureCommandPath() (string, error) {
	dir, err := pluginConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := checkPath(dir, true); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	if _, err := ensureHelpPath(dir); err != nil {
		return "", err
	}

	path := filepath.Join(dir, commandFile)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := f.WriteString(starterCommands); writeErr != nil {
			f.Close()
			return "", writeErr
		}
	} else if errors.Is(err, fs.ErrExist) {
		f, err = openCommandFile(path)
	}
	if err != nil {
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func ensureHelpPath(dir string) (string, error) {
	path := filepath.Join(dir, helpFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := f.Write(embeddedHelp); writeErr != nil {
			f.Close()
			return "", writeErr
		}
	} else if errors.Is(err, fs.ErrExist) {
		if err := checkHelpPath(path); err != nil {
			return "", err
		}
		f, err = os.OpenFile(path, os.O_WRONLY, 0)
	}
	if err != nil {
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func checkHelpPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("HELP.md must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return errors.New("HELP.md must be a regular file")
	}
	return nil
}

func loadHelpLines(path string) ([]string, error) {
	if err := checkHelpPath(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), nil
}

func openCommandFile(path string) (*os.File, error) {
	if err := checkPath(path, false); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR, 0)
}

func checkPath(path string, wantDir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("plugin config directory and commands.md must not be symbolic links")
	}
	if wantDir && !info.IsDir() {
		return errors.New("plugin config path is not a directory")
	}
	if !wantDir && !info.Mode().IsRegular() {
		return errors.New("commands.md is not a regular file")
	}
	return nil
}

func loadEntries(path string) ([]listEntry, error) {
	f, err := openCommandFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []listEntry
	s := bufio.NewScanner(f)
	for lineNo := 1; s.Scan(); lineNo++ {
		line := strings.TrimSuffix(s.Text(), "\r")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			entries = append(entries, listEntry{kind: entryBlank})
			continue
		}
		if err := validateVisibleLine(line); err != nil {
			return nil, fmt.Errorf("commands.md line %d: %w", lineNo, err)
		}
		if strings.HasPrefix(trimmed, "#") {
			entries = append(entries, listEntry{kind: entryHeader, text: trimmed})
			continue
		}
		entries = append(entries, listEntry{kind: entryCommand, text: line})
	}
	return entries, s.Err()
}

func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

func validateVisibleLine(line string) error {
	if !utf8.ValidString(line) {
		return errors.New("invalid UTF-8")
	}
	for _, r := range line {
		if unsafeRune(r) {
			return fmt.Errorf("unsafe invisible/control character U+%04X", r)
		}
	}
	return nil
}

func validateCommand(command string) error {
	return validateVisibleLine(command)
}

func safeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return '�'
		}
		return r
	}, s)
}

// ---------- small dependency-free TUI ----------

func runUI() error {
	path, err := ensureCommandPath()
	if err != nil {
		return err
	}
	entries, err := loadEntries(path)
	if err != nil {
		return err
	}
	target := targetPane()
	if target == "" {
		return errors.New("popup has no target pane; open it through the Command List action")
	}

	restore, err := rawTerminal()
	if err != nil {
		return err
	}
	defer restore()
	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	m := &uiModel{
		entries:     entries,
		selected:    -1,
		commandPath: path,
		helpPath:    filepath.Join(filepath.Dir(path), helpFile),
		configPath:  herdrConfigPath(),
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
			case "close", "escape":
				return nil
			case "help":
				m.help = false
			}
			continue
		}

		switch event {
		case "":
			continue
		case "close", "escape":
			return nil
		case "help":
			m.help = true
		case "section":
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
		case "clear":
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

func (m *uiModel) refresh(selectNewest bool) {
	m.visible = filterEntries(m.entries, string(m.query))
	m.selected = -1
	if selectNewest {
		m.selected = m.lastSelectable()
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
	if m.selected < 0 {
		m.selected = m.lastSelectable()
		return
	}
	for pos := m.selected - 1; pos >= 0; pos-- {
		if m.entries[m.visible[pos]].kind == entryCommand {
			m.selected = pos
			return
		}
	}
}

func (m *uiModel) down() {
	if m.selected < 0 {
		m.selected = m.firstSelectable()
		return
	}
	for pos := m.selected + 1; pos < len(m.visible); pos++ {
		if m.entries[m.visible[pos]].kind == entryCommand {
			m.selected = pos
			return
		}
	}
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
	return exec.Command(herdrBin(), "pane", "run", pane, command).Run()
}

func draw(m *uiModel, rows, cols int) {
	if m.help {
		drawHelp(m, rows, cols)
		return
	}
	if rows < 8 {
		rows = 8
	}
	if cols < 30 {
		cols = 30
	}

	height, offset := rows-5, 0
	if len(m.visible) > height {
		if m.selected < 0 {
			offset = 0
		} else {
			offset = min(max(m.selected-height+1, 0), len(m.visible)-height)
		}
	}
	end := min(offset+height, len(m.visible))

	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[2J\x1b[H\x1b[1mCommand List\x1b[0m  %d commands\r\nSearch: ", countCommands(m.entries))
	if len(m.query) == 0 {
		b.WriteString("\x1b[2mtype to filter\x1b[0m")
	} else {
		b.WriteString(truncate(string(m.query), cols-8))
	}
	b.WriteString("\r\n\r\n")

	for pos := offset; pos < end; pos++ {
		entry := m.entries[m.visible[pos]]
		switch entry.kind {
		case entryBlank:
			b.WriteString("\r\n")
		case entryHeader:
			fmt.Fprintf(&b, "  \x1b[1;36m%s\x1b[0m\r\n", truncate(entry.text, cols-2))
		case entryCommand:
			prefix, suffix := "  ", ""
			if pos == m.selected {
				prefix, suffix = "\x1b[7m> ", "\x1b[0m"
			}
			fmt.Fprintf(&b, "%s%s%s\r\n", prefix, truncate(entry.text, cols-2), suffix)
		}
	}
	for i := end - offset; i < height; i++ {
		b.WriteString("\r\n")
	}
	b.WriteString("\x1b[2m↑/↓ select | Ctrl+Space section | Enter run | ? help | Esc/Ctrl+C close\x1b[0m")
	fmt.Print(b.String())
}

func drawHelp(m *uiModel, rows, cols int) {
	if rows < 12 {
		rows = 12
	}
	if cols < 40 {
		cols = 40
	}
	lines, err := loadHelpLines(m.helpPath)
	if err != nil {
		lines = []string{"Unable to read HELP.md: " + safeDisplay(err.Error())}
	}

	var b strings.Builder
	b.WriteString("\x1b[2J\x1b[H")
	for _, line := range lines {
		b.WriteString(truncate(safeDisplay(line), cols))
		b.WriteString("\r\n")
	}
	fmt.Print(b.String())
}

func truncate(s string, width int) string {
	r := []rune(s)
	if width <= 0 {
		return ""
	}
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
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

func readKey() (event string, r rune, err error) {
	b, ok, err := readByte()
	if err != nil || !ok {
		return "", 0, err
	}
	switch b {
	case 0x00: // Ctrl+Space (NUL in terminals)
		return "section", 0, nil
	case 0x03, 0x11: // Ctrl+C/Q
		return "close", 0, nil
	case 0x15: // Ctrl+U
		return "clear", 0, nil
	case '?':
		return "help", 0, nil
	case '\r', '\n':
		return "enter", 0, nil
	case 0x7f, 0x08:
		return "backspace", 0, nil
	case 0x1b:
		second, ok, err := readByte()
		if err != nil || !ok {
			return "escape", 0, err
		}
		if second != '[' && second != 'O' {
			return "escape", 0, nil
		}
		third, ok, err := readByte()
		if err != nil || !ok {
			return "escape", 0, err
		}
		if third == 'A' {
			return "up", 0, nil
		}
		if third == 'B' {
			return "down", 0, nil
		}
		return "", 0, nil
	}
	if b < utf8.RuneSelf {
		return "rune", rune(b), nil
	}

	buf := []byte{b}
	for !utf8.FullRune(buf) && len(buf) < utf8.UTFMax {
		next, ok, err := readByte()
		if err != nil || !ok {
			return "", 0, err
		}
		buf = append(buf, next)
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return "", 0, nil
	}
	return "rune", r, nil
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

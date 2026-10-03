package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateVisibleLine(t *testing.T) {
	good := []string{"git status", "# Git", "printf 'hello world'", "echo hello ✓", "git log --oneline | head"}
	for _, line := range good {
		if err := validateVisibleLine(line); err != nil {
			t.Fatalf("%q should be allowed: %v", line, err)
		}
	}

	bad := []string{"echo\tbad", "echo\x1bbad", "echo \u200bhidden", "echo \u202Ehidden", "a\u2028b"}
	for _, line := range bad {
		if err := validateVisibleLine(line); err == nil {
			t.Fatalf("%q should be rejected", line)
		}
	}
}

func TestLoadEntriesPreservesSectionsAndSpacing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, commandFile)
	content := "# Git\r\ngit status\r\n\r\n# Docker\r\ndocker compose ps\r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []listEntry{
		{kind: entryHeader, text: "# Git"},
		{kind: entryCommand, text: "git status"},
		{kind: entryBlank},
		{kind: entryHeader, text: "# Docker"},
		{kind: entryCommand, text: "docker compose ps"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestLoadRejectsUnsafeHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, commandFile)
	if err := os.WriteFile(path, []byte("# Git \u202Ehidden\ngit status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadEntries(path)
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line-1 safety error, got %v", err)
	}
}

func TestEnsureCommandPathCreatesStarterAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)

	path, err := ensureCommandPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "commands.md" {
		t.Fatalf("path = %q, want commands.md", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != starterCommands {
		t.Fatalf("starter content = %q, want %q", content, starterCommands)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "# command-list plugin" || lines[1] == "" {
		t.Fatalf("starter must put the first command directly below the heading: %#v", lines)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config dir mode = %o, want 700", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("commands.md mode = %o, want 600", got)
	}
}

func TestEnsureDoesNotOverwriteExistingCommands(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, commandFile)
	const existing = "# Mine\necho keep-me\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)

	if _, err := ensureCommandPath(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("existing commands were changed: %q", got)
	}
}

func TestEnsureRejectsSymlinkConfigDir(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	linkDir := filepath.Join(base, "link")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", linkDir)
	if _, err := ensureCommandPath(); err == nil {
		t.Fatal("expected symlink config directory to be rejected")
	}
}

func TestOpenCommandFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, commandFile)
	if err := os.WriteFile(real, []byte("git status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := openCommandFile(link); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestFilterEntriesKeepsMatchingSectionHeader(t *testing.T) {
	entries := []listEntry{
		{kind: entryHeader, text: "# Git"},
		{kind: entryCommand, text: "git status"},
		{kind: entryCommand, text: "git log --oneline"},
		{kind: entryHeader, text: "# Docker"},
		{kind: entryCommand, text: "docker compose ps"},
	}
	got := filterEntries(entries, "log")
	want := []int{0, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestInitialUpAndDownSelectFromOppositeEnds(t *testing.T) {
	entries := []listEntry{
		{kind: entryHeader, text: "# One"},
		{kind: entryCommand, text: "first"},
		{kind: entryHeader, text: "# Two"},
		{kind: entryCommand, text: "second"},
	}

	upModel := &uiModel{entries: entries, selected: -1}
	upModel.refresh(false)
	if _, ok := upModel.current(); ok {
		t.Fatal("nothing should be selected initially")
	}
	upModel.up()
	if command, ok := upModel.current(); !ok || command != "second" {
		t.Fatalf("Up should select last command, got %q, %v", command, ok)
	}

	downModel := &uiModel{entries: entries, selected: -1}
	downModel.refresh(false)
	downModel.down()
	if command, ok := downModel.current(); !ok || command != "first" {
		t.Fatalf("Down should select first command, got %q, %v", command, ok)
	}
}

func TestNavigationSkipsHeadersAndDoesNotDropSelectionAtEnds(t *testing.T) {
	m := &uiModel{entries: []listEntry{
		{kind: entryHeader, text: "# One"},
		{kind: entryCommand, text: "first"},
		{kind: entryHeader, text: "# Two"},
		{kind: entryCommand, text: "second"},
	}, selected: -1}
	m.refresh(false)
	m.down()
	m.down()
	if command, ok := m.current(); !ok || command != "second" {
		t.Fatalf("second Down should select second command, got %q, %v", command, ok)
	}
	m.down()
	if command, ok := m.current(); !ok || command != "second" {
		t.Fatalf("Down at end should keep last command selected, got %q, %v", command, ok)
	}
	m.up()
	if command, ok := m.current(); !ok || command != "first" {
		t.Fatalf("Up should skip header and select first command, got %q, %v", command, ok)
	}
	m.up()
	if command, ok := m.current(); !ok || command != "first" {
		t.Fatalf("Up at start should keep first command selected, got %q, %v", command, ok)
	}
}

func TestNextSectionJumpsToFirstCommandAndWraps(t *testing.T) {
	m := &uiModel{entries: []listEntry{
		{kind: entryHeader, text: "# Git"},
		{kind: entryCommand, text: "git status"},
		{kind: entryCommand, text: "git log"},
		{kind: entryHeader, text: "# Empty"},
		{kind: entryBlank},
		{kind: entryHeader, text: "# Docker"},
		{kind: entryCommand, text: "docker compose ps"},
	}, selected: -1}
	m.refresh(false)

	m.nextSection()
	if command, ok := m.current(); !ok || command != "git status" {
		t.Fatalf("first section jump = %q, %v", command, ok)
	}
	m.nextSection()
	if command, ok := m.current(); !ok || command != "docker compose ps" {
		t.Fatalf("second section jump = %q, %v", command, ok)
	}
	m.nextSection()
	if command, ok := m.current(); !ok || command != "git status" {
		t.Fatalf("wrapped section jump = %q, %v", command, ok)
	}
}

func TestNextSectionClearsSearch(t *testing.T) {
	m := &uiModel{entries: []listEntry{
		{kind: entryHeader, text: "# Git"},
		{kind: entryCommand, text: "git status"},
		{kind: entryHeader, text: "# Docker"},
		{kind: entryCommand, text: "docker compose ps"},
	}, query: []rune("docker"), selected: -1}
	m.refresh(false)
	m.nextSection()
	if len(m.query) != 0 {
		t.Fatalf("section jump should clear search, got %q", string(m.query))
	}
	if command, ok := m.current(); !ok || command != "git status" {
		t.Fatalf("after clearing search first section should be selected, got %q, %v", command, ok)
	}
}

func TestLoadRejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, commandFile)
	if err := os.WriteFile(path, []byte{'e', 'c', 'h', 'o', ' ', 0xff, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadEntries(path)
	if err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 error, got %v", err)
	}
}

func TestEnsureHardensExistingPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, commandFile)
	if err := os.WriteFile(path, []byte("git status\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)

	gotPath, err := ensureCommandPath()
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config dir mode = %o, want 700", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("commands.md mode = %o, want 600", got)
	}
}

func TestEnsureRejectsSymlinkCommandFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, commandFile)
	if err := os.WriteFile(real, []byte("git status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	if _, err := ensureCommandPath(); err == nil {
		t.Fatal("expected symlink commands.md to be rejected")
	}
}

func TestLoadRejectsDirectoryAsCommandFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, commandFile)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEntries(path); err == nil {
		t.Fatal("expected directory used as commands.md to be rejected")
	}
}

func TestSubmitCommandRevalidatesUnsafeInput(t *testing.T) {
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "does-not-exist"))
	err := submitCommand("pane-1", "echo \u200bhidden")
	if err == nil || !strings.Contains(err.Error(), "unsafe invisible/control character") {
		t.Fatalf("expected validation error before Herdr execution, got %v", err)
	}
}

func TestUnsafeRune(t *testing.T) {
	if unsafeRune('a') || unsafeRune('✓') {
		t.Fatal("normal visible runes should be accepted")
	}
	for _, r := range []rune{'\n', '\t', '\u200b', '\u202e', '\u2028', '\u2029'} {
		if !unsafeRune(r) {
			t.Fatalf("U+%04X should be rejected", r)
		}
	}
}

func TestSafeDisplayReplacesUnsafeRunes(t *testing.T) {
	got := safeDisplay("/tmp/ok\x1b[31m\u202e")
	if strings.ContainsAny(got, "\x1b\u202e") {
		t.Fatalf("unsafe terminal characters remained in %q", got)
	}
	if got != "/tmp/ok�[31m�" {
		t.Fatalf("safeDisplay = %q", got)
	}
}

func TestOpenPopupDoesNotPassTargetPane(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	fakeHerdr := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HERDR_TEST_ARGS\"\n"
	if err := os.WriteFile(fakeHerdr, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_TEST_ARGS", argsPath)
	t.Setenv(targetEnvKey, "w2:p1")

	if err := openPopup(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := string(data)
	if strings.Contains(args, "--target-pane") {
		t.Fatalf("popup must not pass --target-pane: %q", args)
	}
	for _, want := range []string{"plugin\n", "pane\n", "open\n", "--plugin\n", "herdr.command-list\n", "--entrypoint\n", "command-list\n", "--env\n", targetEnvKey + "=w2:p1\n", "--focus\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("popup args missing %q in %q", want, args)
		}
	}
}

func TestTargetPaneUsesActivePaneFallback(t *testing.T) {
	t.Setenv(targetEnvKey, "")
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "")
	t.Setenv("HERDR_ACTIVE_PANE_ID", "w9:p3")
	if got := targetPane(); got != "w9:p3" {
		t.Fatalf("targetPane = %q, want w9:p3", got)
	}
}

func TestEnsureCommandPathCreatesHelpFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)

	if _, err := ensureCommandPath(); err != nil {
		t.Fatal(err)
	}
	helpPath := filepath.Join(dir, helpFile)
	content, err := os.ReadFile(helpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(embeddedHelp) {
		t.Fatalf("HELP.md content differs from embedded help")
	}
	info, err := os.Stat(helpPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("HELP.md mode = %o, want 600", got)
	}
}

func TestEnsureRejectsSymlinkHelpFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin-config")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real-help.md")
	link := filepath.Join(dir, helpFile)
	if err := os.WriteFile(real, []byte("help\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	if _, err := ensureCommandPath(); err == nil {
		t.Fatal("expected symlink HELP.md to be rejected")
	}
}

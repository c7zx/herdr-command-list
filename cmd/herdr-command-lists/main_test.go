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
	path := filepath.Join(dir, "commands.md")
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
	path := filepath.Join(dir, "commands.md")
	if err := os.WriteFile(path, []byte("# Git \u202Ehidden\ngit status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadEntries(path)
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("expected line-1 safety error, got %v", err)
	}
}

func TestOpenCommandFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, "commands.md")
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

func TestNavigationSkipsHeadersAndWrapsAtBothEnds(t *testing.T) {
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
	if command, ok := m.current(); !ok || command != "first" {
		t.Fatalf("Down at end should wrap to first command, got %q, %v", command, ok)
	}
	m.up()
	if command, ok := m.current(); !ok || command != "second" {
		t.Fatalf("Up at start should wrap to last command, got %q, %v", command, ok)
	}
	m.up()
	if command, ok := m.current(); !ok || command != "first" {
		t.Fatalf("Up should skip header and select first command, got %q, %v", command, ok)
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
	path := filepath.Join(dir, "commands.md")
	if err := os.WriteFile(path, []byte{'e', 'c', 'h', 'o', ' ', 0xff, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadEntries(path)
	if err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 error, got %v", err)
	}
}

func TestLoadRejectsDirectoryAsCommandFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.md")
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
	for _, want := range []string{"plugin\n", "pane\n", "open\n", "--plugin\n", "herdr.command-lists\n", "--entrypoint\n", "command-lists\n", "--env\n", targetEnvKey + "=w2:p1\n", "--focus\n"} {
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

func TestInstalledRootFromJSON(t *testing.T) {
	input := `{"id":"cli:plugin","result":{"plugins":[{"plugin_id":"other.plugin","plugin_root":"/tmp/other"},{"plugin_id":"herdr.command-lists","plugin_root":"/tmp/path with spaces"}]}}`
	got, err := installedRootFromJSON(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/path with spaces" {
		t.Fatalf("installedRootFromJSON = %q", got)
	}

	got, err = installedRootFromJSON(strings.NewReader(`{"result":{"plugins":[]}}`))
	if err != nil || got != "" {
		t.Fatalf("empty plugin list = %q, %v", got, err)
	}

	if _, err := installedRootFromJSON(strings.NewReader(`not json`)); err == nil {
		t.Fatal("invalid plugin list JSON should fail")
	}
}

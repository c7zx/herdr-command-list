package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func setInitConfig(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "plugin-config")
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	t.Setenv("HERDR_BIN_PATH", filepath.Join(t.TempDir(), "no-herdr"))
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s permissions = %o, want %o", path, got, want)
	}
}

func TestEnsureListsCreatesThreePrivateStarterListsAndCurrentHelp(t *testing.T) {
	dir := setInitConfig(t)
	listsDir, err := ensureListsDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "lists"); listsDir != want {
		t.Fatalf("lists path = %q, want %q", listsDir, want)
	}
	lists, err := loadLists(listsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 3 {
		t.Fatalf("fresh config should contain three example lists, got %d", len(lists))
	}
	wantNames := []string{"Main", "Systems", "More"}
	for i, want := range wantNames {
		if lists[i].name != want {
			t.Fatalf("starter order = %v, want %v", []string{lists[0].name, lists[1].name, lists[2].name}, wantNames)
		}
		wantEdit := "/lists/" + want + ".md"
		foundEdit := false
		for _, entry := range lists[i].entries {
			if entry.kind == entryCommand && strings.Contains(entry.text, wantEdit) {
				foundEdit = true
				break
			}
		}
		if !foundEdit {
			t.Fatalf("starter %q has no self-edit command containing %q", want, wantEdit)
		}
	}
	assertMode(t, dir, 0o700)
	assertMode(t, listsDir, 0o700)
	for _, list := range lists {
		if countCommands(list.entries) < 2 {
			t.Fatalf("starter %q has too few example commands", list.name)
		}
		assertMode(t, list.path, 0o600)
	}
	helpPath := filepath.Join(dir, helpFile)
	data, err := os.ReadFile(helpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(embeddedHelp) {
		t.Fatal("generated help differs from this release's embedded help")
	}
	assertMode(t, helpPath, 0o600)
}

func TestEmbeddedStarterListsMatchReleaseExamples(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{
			name: "Main.md",
			want: `#=========================== MAIN ===========================#

# Edit This List
nano "$(herdr plugin config-dir herdr.command-lists)/lists/Main.md"

#------------------- COMMAND LISTS PLUGIN -------------------#

# Plugin
nano ~/.config/herdr/config.toml
cat "$(herdr plugin config-dir herdr.command-lists)/HELP.md"
cd "$(herdr plugin config-dir herdr.command-lists)/lists"

#--------------------- EXAMPLE COMMANDS ---------------------#

# Examples
whoami
date

#---------------------- MORE EXAMPLES -----------------------#

# More
printf 'Hello World!\n'
cd "$HOME"`,
		},
		{
			name: "Systems.md",
			want: `#=========================== Systems ===========================#

# Edit This List
nano "$(herdr plugin config-dir herdr.command-lists)/lists/Systems.md"

# System 1
 uptime

# System 2
  df -h

# System 3
   hostname

# System 4
whoami
 uptime
  printf 'Indented lines use a different color.\n'`,
		},
		{
			name: "More.md",
			want: `#=========================== More ===========================#

# Edit This List
nano "$(herdr plugin config-dir herdr.command-lists)/lists/More.md"

# User
whoami

# Here
pwd

# Today
date '+%Y-%m-%d'

# Time
date '+%H:%M:%S'

# Hello
printf 'Hello from Command Lists!\n'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := embeddedExamples.ReadFile("assets/examples/" + tt.name)
			if err != nil {
				t.Fatal(err)
			}

			got := strings.TrimRight(string(data), " \t\r\n")
			want := strings.TrimRight(tt.want, " \t\r\n")
			if got != want {
				t.Fatalf("%s differs from the intended starter:\n%s", tt.name, data)
			}
		})
	}
}

func TestEnsureListsPreservesEditsAndDeletionsAndRefreshesHelp(t *testing.T) {
	dir := setInitConfig(t)
	listsDir, err := ensureListsDir()
	if err != nil {
		t.Fatal(err)
	}
	lists, err := loadLists(listsDir)
	if err != nil || len(lists) < 2 {
		t.Fatalf("initial lists = %#v, %v", lists, err)
	}
	const custom = "# My list\nprintf 'keep this exactly\\n'\n"
	mustWrite(t, lists[0].path, custom)
	if err := os.Remove(lists[1].path); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, helpFile), "old release help\n"+strings.Repeat("obsolete\n", 1000))
	if _, err := ensureListsDir(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lists[0].path)
	if err != nil || string(data) != custom {
		t.Fatalf("edited list was changed: %q, %v", data, err)
	}
	if _, err := os.Lstat(lists[1].path); !os.IsNotExist(err) {
		t.Fatalf("deleted example was recreated: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, helpFile))
	if err != nil || string(data) != string(embeddedHelp) {
		t.Fatalf("old help was not fully refreshed: %v", err)
	}
}

func TestEnsureListsRejectsSymlinkPaths(t *testing.T) {
	for _, name := range []string{"config", "lists", "help"} {
		t.Run(name, func(t *testing.T) {
			dir := setInitConfig(t)
			if name == "config" {
				if err := os.Symlink(t.TempDir(), dir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				var link, target string
				switch name {
				case "lists":
					link, target = filepath.Join(dir, "lists"), t.TempDir()
				case "help":
					link, target = filepath.Join(dir, helpFile), filepath.Join(t.TempDir(), "private.md")
					mustWrite(t, target, "whoami\n")
				}
				if err := os.Symlink(target, link); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ensureListsDir(); err == nil {
				t.Fatalf("symbolic link at %s was accepted", name)
			}
		})
	}
}

func TestEnsureListsKeepsExistingListsWithoutAddingStarters(t *testing.T) {
	dir := setInitConfig(t)
	listsDir := filepath.Join(dir, "lists")
	if err := os.MkdirAll(listsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const custom = "# Personal\nprintf 'keep me\\n'\n"
	mustWrite(t, filepath.Join(listsDir, "Personal.md"), custom)
	if _, err := ensureListsDir(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(listsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Personal.md" {
		t.Fatalf("existing list directory was seeded or changed: %v", entries)
	}
	data, err := os.ReadFile(filepath.Join(listsDir, "Personal.md"))
	if err != nil || string(data) != custom {
		t.Fatalf("existing list changed: %q, %v", data, err)
	}
}

func TestEnsureListsSeedsExistingEmptyDirectory(t *testing.T) {
	dir := setInitConfig(t)
	listsDir := filepath.Join(dir, "lists")
	if err := os.MkdirAll(listsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureListsDir(); err != nil {
		t.Fatal(err)
	}
	lists, err := loadLists(listsDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{lists[0].name, lists[1].name, lists[2].name}; !reflect.DeepEqual(got, []string{"Main", "Systems", "More"}) {
		t.Fatalf("empty retained directory starter order = %v", got)
	}
}

func TestEnsureListsHardensExistingDirectories(t *testing.T) {
	dir := setInitConfig(t)
	listsDir := filepath.Join(dir, "lists")
	if err := os.MkdirAll(listsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, listsDir} {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ensureListsDir(); err != nil {
		t.Fatal(err)
	}
	assertMode(t, dir, 0o700)
	assertMode(t, listsDir, 0o700)
}

func TestLoadListsSortedAndReloadedFromDisk(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Work.md"), "# Work\nwhoami\n")
	mustWrite(t, filepath.Join(dir, "Local.md"), "date\n")
	mustWrite(t, filepath.Join(dir, "notes.txt"), "this is not a command list\n")
	lists, err := loadLists(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []commandList{
		{name: "Local", path: filepath.Join(dir, "Local.md"), entries: []listEntry{{kind: entryCommand, text: "date"}}},
		{name: "Work", path: filepath.Join(dir, "Work.md"), entries: []listEntry{{kind: entryHeader, text: "# Work"}, {kind: entryCommand, text: "whoami"}}},
	}
	if !reflect.DeepEqual(lists, want) {
		t.Fatalf("lists = %#v, want %#v", lists, want)
	}
	mustWrite(t, filepath.Join(dir, "Local.md"), "pwd\n")
	if err := os.Remove(filepath.Join(dir, "Work.md")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "Added.md"), "printf 'added\\n'\n")
	lists, err = loadLists(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || lists[0].name != "Added" || lists[1].name != "Local" || lists[1].entries[0].text != "pwd" {
		t.Fatalf("lists did not reflect add/delete/edit: %#v", lists)
	}
}

func TestLoadListsRejectsUnsafeNamesAndSymlinks(t *testing.T) {
	for _, name := range []string{"bad\x1b[31m.md", "bad\u200b.md", "bad\n.md", string([]byte{'b', 0xff, '.', 'm', 'd'})} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, name), "date\n")
			if _, err := loadLists(dir); err == nil {
				t.Fatalf("unsafe list name %q was accepted", name)
			}
		})
	}
	dir := t.TempDir()
	real := filepath.Join(t.TempDir(), "outside.md")
	mustWrite(t, real, "date\n")
	if err := os.Symlink(real, filepath.Join(dir, "Link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLists(dir); err == nil {
		t.Fatal("symbolic command list was accepted")
	}
}

func TestLoadListsFailsOnUnsafeContentWithSourceLine(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Broken.md"), "# Safe header\necho \u202Ehidden\n")
	_, err := loadLists(dir)
	if err == nil || !strings.Contains(err.Error(), "Broken.md") || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected source filename and line in validation error, got %v", err)
	}
}

func TestLoadListsRejectsFIFOInsteadOfWaitingForInput(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "Pipe.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLists(dir); err == nil {
		t.Fatal("FIFO was accepted as a command list")
	}
}

func TestLoadEntriesPreservesLeadingSpaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Systems.md")
	mustWrite(t, path, "# Colors\n date\n  whoami\n   printf 'indented\\n'\n")
	entries, err := loadEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{" date", "  whoami", "   printf 'indented\\n'"}
	for i, command := range want {
		if got := entries[i+1]; got.kind != entryCommand || got.text != command {
			t.Fatalf("indented command = %#v, want %q", got, command)
		}
	}
}

func TestLoadListsDoesNotModifyReadOnlyUserFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ReadOnly.md")
	const content = "# Managed elsewhere\n  date\n"
	mustWrite(t, path, content)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	lists, err := loadLists(dir)
	if err != nil || len(lists) != 1 {
		t.Fatalf("read-only command list could not be read: %#v, %v", lists, err)
	}
	assertMode(t, path, 0o444)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("read-only list was changed: %q, %v", data, err)
	}
}

func TestLoadEntriesRejectsOversizedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Large.md")
	mustWrite(t, path, strings.Repeat("date\n", (1<<20)/5+1))
	if _, err := loadEntries(path); err == nil {
		t.Fatal("list over the one-MiB limit was accepted")
	}
}

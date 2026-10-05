package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestShortcutInstallPreservesCustomKeyAndUnrelatedConfig(t *testing.T) {
	original := "# Keep this comment\r\n[[keys.command]] # custom shortcut\r\nkey = 'prefix+x'\r\ntype = \"plugin_action\"\r\ncommand = 'herdr.command-lists.toggle' # keep this\r\ndescription = \"My shortcuts\"\r\n\r\n[theme]\r\nname = \"dark # literal\"\r\n"
	got, _, err := editShortcutConfig(original, "install")
	if err != nil || got != original {
		t.Fatalf("installation changed an existing shortcut or unrelated content:\n%s\nerror: %v", got, err)
	}
	again, _, err := editShortcutConfig(got, "install")
	if err != nil || again != got {
		t.Fatalf("installation is not idempotent: %v", err)
	}
}

func TestShortcutInstallHonorsConflictsAndExistingShortcuts(t *testing.T) {
	for _, source := range []string{
		"[[keys.command]]\nkey = \"CTRL+Y\" # occupied\ntype = \"send\"\ncommand = \"something else\"\n",
		"[[keys.command]]\nkey = \"prefix+j\"\ntype = \"plugin_action\"\ncommand = \"herdr.command-lists.toggle\"\n",
		"[[ 'keys' . \"command\" ]] # quoted names\nkey = 'alt+c'\ntype = 'plugin_action'\ncommand = 'herdr.command-lists.toggle'\n",
		"[keys]\nresize_pane_left = 'ctrl+y'\n",
		"keys.resize_pane_left = 'control+y'\n",
		"[\"keys\"]\nresize_pane_left = ['ctrl+y', 'alt+y']\n",
		"[keys.normal]\nresize_pane_left = 'ctrl+y'\n",
		"[[keys.command]]\nkey = 'control+y'\ntype = 'send'\ncommand = 'test'\n",
	} {
		got, _, err := editShortcutConfig(source, "install")
		if err != nil || got != source {
			t.Errorf("existing key was changed or duplicated: %q; %v", got, err)
		}
	}
}

func TestShortcutDefaultPreservesMultilineValues(t *testing.T) {
	source := "[theme]\ncolors = [\n [\"red\", \"blue\"], # a nested array\n [\"[[keys.command]]\", \"green\"]\n]\nname = \"literal \\\"quote\\\" # kept\""
	got, _, err := editShortcutConfig(source, "install")
	if err != nil || !strings.HasPrefix(got, source+"\n\n[[keys.command]]") {
		t.Fatalf("failed to preserve existing values: %q; %v", got, err)
	}
	if strings.Count(got, `key = "ctrl+y"`) != 1 {
		t.Fatalf("expected exactly one default key: %q", got)
	}
}

func TestShortcutRemovalOwnsOnlyExactPluginActions(t *testing.T) {
	other := "[[keys.command]]\nkey = \"ctrl+y\"\ntype = \"send\"\ncommand = \"herdr.command-lists.toggle\"\n\n[[keys.command]]\nkey = \"alt+x\"\ntype = \"plugin_action\"\ncommand = \"herdr.command-lists.toggle.extra\"\n\n[[keys.command]]\nkey = \"prefix+c\"\ntype = \"plugin_action\"\ncommand = \"other.plugin.toggle\"\n\n[theme]\nname = \"dark\"\n"
	owned := "# Personal comment survives\n[[keys.command]]\nkey = \"alt+c\"\ntype = \"plugin_action\"\ncommand = \"herdr.command-lists.toggle\"\n\n"
	got, _, err := editShortcutConfig(owned+other, "remove")
	if err != nil || got != "# Personal comment survives\n\n"+other {
		t.Fatalf("removal changed unrelated config:\n%s\nerror: %v", got, err)
	}
}

func TestShortcutRefusesAmbiguousConfig(t *testing.T) {
	for _, source := range []string{
		"description = '''\n[[keys.command]]\n'''\n",
		"[[keys.command]]\nkey = \"ctrl+y\"\nkey = \"ctrl+x\"\n",
		"[[keys.command]]\nkey = [\"ctrl+y\"]\n",
		"[[keys.command]]\nkey = \"ctrl+y\"\ntype = \"plugin_action\"\ncommand = \"herdr.command-lists.toggle\"\n[keys.command.extra]\nvalue = 1\n",
		"[theme]\nname = \"unterminated\n",
		"[theme]\ncolors = [\n\"red\"\n",
		"keys = { command = [{key = 'ctrl+y'}] }\n",
		"keys.command = [{key = 'ctrl+y'}]\n",
		"\"keys\" . 'command' = [{key = 'ctrl+y'}]\n",
		"[keys]\n'command' = [{key = 'ctrl+y'}]\n",
		"[keys.command]\nkey = 'ctrl+y'\n",
	} {
		for _, mode := range []string{"install", "remove"} {
			if _, _, err := editShortcutConfig(source, mode); err == nil {
				t.Errorf("accepted ambiguous %s config: %q", mode, source)
			}
		}
	}
}

func TestShortcutDoesNotConfuseLiteralDottedTable(t *testing.T) {
	source := "[[\"keys.command\"]]\nkey = 'alt+x'\ntype = 'plugin_action'\ncommand = 'herdr.command-lists.toggle'\n"
	got, _, err := editShortcutConfig(source, "remove")
	if err != nil || got != source {
		t.Fatalf("edited a different TOML table: %q; %v", got, err)
	}
}

func TestConfigureShortcutBackupAndPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	source := "[theme]\nname = 'dark'\n"
	if err := os.WriteFile(path, []byte(source), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	status, err := configureShortcut("install", path)
	if err != nil || !strings.Contains(status, "Config backup:") {
		t.Fatalf("missing backup: %q; %v", status, err)
	}
	backups, err := filepath.Glob(path + ".command-lists-backup-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one backup, got %v; %v", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil || string(backup) != source {
		t.Fatalf("backup differs: %q; %v", backup, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("existing file permissions changed: %v; %v", info, err)
	}
	info, err = os.Stat(backups[0])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("backup must be private: %v; %v", info, err)
	}
	if _, err := configureShortcut("install", path); err != nil {
		t.Fatal(err)
	}
	backups, _ = filepath.Glob(path + ".command-lists-backup-*")
	if len(backups) != 1 {
		t.Fatal("idempotent install created another backup")
	}
}

func TestConfigureShortcutNewAndMissingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.toml")
	if _, err := configureShortcut("remove", path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("remove created a missing config directory")
	}
	if _, err := configureShortcut("install", path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("new config must be private: %v; %v", info, err)
	}
	if _, err := configureShortcut("unknown", path); err == nil {
		t.Fatal("accepted unknown mode")
	}
}

func TestConfigureShortcutUnsafePathsRemainUntouched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.toml")
	if err := os.WriteFile(target, []byte("# Do not modify\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"install", "remove"} {
		if _, err := configureShortcut(mode, link); err == nil {
			t.Errorf("%s accepted a symlink", mode)
		}
	}
	contents, _ := os.ReadFile(target)
	if string(contents) != "# Do not modify\n" {
		t.Fatal("symlink target was modified")
	}
	if _, err := configureShortcut("install", dir); err == nil {
		t.Fatal("accepted a directory as a config file")
	}
	parentLink := filepath.Join(dir, "linked-dir")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := configureShortcut("install", filepath.Join(parentLink, "new.toml")); err == nil {
		t.Fatal("accepted symlinked config parent for a write")
	}
}

func TestConfigureShortcutAmbiguityDoesNotWriteBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	source := "description = '''multiline'''\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := configureShortcut("install", path); err == nil {
		t.Fatal("expected an unsupported-syntax error")
	}
	contents, _ := os.ReadFile(path)
	backups, _ := filepath.Glob(path + ".command-lists-backup-*")
	if string(contents) != source || len(backups) != 0 {
		t.Fatal("unsupported config was changed or backed up")
	}
}

func TestConfigureShortcutRejectsOversizedAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large.toml")
	if err := os.WriteFile(large, bytes.Repeat([]byte(" "), (2<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(dir, "pipe.toml")
	if err := syscall.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{large, pipe} {
		if _, err := configureShortcut("install", path); err == nil {
			t.Errorf("accepted unsafe config %s", path)
		}
		backups, _ := filepath.Glob(path + ".command-lists-backup-*")
		if len(backups) != 0 {
			t.Errorf("created backups before rejecting %s", path)
		}
	}
}

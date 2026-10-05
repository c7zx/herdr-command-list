package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type lifecycleFixture struct {
	base, source, config, log, helper string
	env                               []string
}

// Exercise the shell scripts without touching a real Herdr installation or
// recursively running the real Go toolchain. The config editor is tested
// separately with real files.
func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	source := filepath.Join(home, ".herdr-plugins", "plugin-source")
	bin := filepath.Join(base, "bin")
	config := filepath.Join(home, ".config", "herdr", "plugins", "config", pluginID)
	for _, dir := range []string{source, bin, filepath.Dir(config)} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate lifecycle test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	for _, name := range []string{"install.sh", "uninstall.sh", "herdr-plugin.toml"} {
		body, err := os.ReadFile(filepath.Join(repoRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(source, name), string(body))
	}

	log := filepath.Join(base, "calls.log")
	helper := filepath.Join(base, "helper-stub")
	write(helper, `#!/bin/sh
set -eu
printf 'helper %s\n' "$*" >> "$LIFECYCLE_LOG"
if [ "$1" = installed-root ]; then printf '%s' "${LIFECYCLE_INSTALLED_ROOT:-}"; exit 0; fi
if [ "${LIFECYCLE_FAIL:-}" = shortcut ] && [ "$1" = shortcut-config ]; then exit 7; fi
if [ "$1" = init ]; then printf '%s\n' "$LIFECYCLE_CONFIG/lists"; fi
`)
	write(filepath.Join(bin, "go"), `#!/bin/sh
set -eu
printf 'go %s %s\n' "${GOTOOLCHAIN:-}" "$1" >> "$LIFECYCLE_LOG"
[ "${LIFECYCLE_FAIL:-}" != "$1" ] || exit 7
if [ "$1" = build ]; then
    while [ "$1" != -o ]; do shift; done
    shift
    cp "$LIFECYCLE_HELPER" "$1"
    chmod 700 "$1"
fi
`)
	herdr := filepath.Join(bin, "herdr with spaces")
	write(herdr, `#!/bin/sh
set -eu
printf 'herdr %s\n' "$*" >> "$LIFECYCLE_LOG"
if [ "$1" = plugin ] && [ "$2" = config-dir ]; then
    printf '%s\n' "$LIFECYCLE_CONFIG"
    exit 0
fi
if [ "$1" = plugin ] && [ "$2" = list ]; then
    printf '%s\n' '{"id":"cli:plugin","result":{"plugins":[]}}'
    exit 0
fi
if [ "$1" = plugin ] && [ "$2" = link ]; then
    printf '%s\n' '{"id":"cli:plugin","result":"plugin_link_json"}'
fi
if [ "${LIFECYCLE_FAIL:-}" = reload ] && [ "$1" = server ]; then exit 7; fi
`)
	env := append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "HERDR_CONFIG_PATH=",
		"HERDR_BIN_PATH="+herdr, "LIFECYCLE_HELPER="+helper, "LIFECYCLE_LOG="+log,
		"LIFECYCLE_CONFIG="+config, "LIFECYCLE_FAIL=", "LIFECYCLE_INSTALLED_ROOT=")
	return lifecycleFixture{base: base, source: source, config: config, log: log, helper: helper, env: env}
}

func runLifecycleScript(t *testing.T, f lifecycleFixture, script string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", filepath.Join(f.source, script))
	cmd.Dir = f.source
	cmd.Env = append(append([]string{}, f.env...), extraEnv...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestLifecycleInstallSuppressesLinkJSON(t *testing.T) {
	f := newLifecycleFixture(t)
	output, err := runLifecycleScript(t, f, "install.sh")
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "plugin_link_json") || strings.Contains(strings.ToLower(output), "update") || strings.Contains(output, "Upgrading") {
		t.Fatalf("installer printed obsolete/noisy output: %s", output)
	}
	calls, _ := os.ReadFile(f.log)
	want := "go local test\ngo local build\nherdr plugin list --plugin herdr.command-lists --json\nhelper installed-root\nherdr plugin link .\nhelper init\nhelper shortcut-config install\nherdr server reload-config\n"
	if string(calls) != want {
		t.Fatalf("unexpected install actions:\n%s", calls)
	}
	if _, err := os.Stat(filepath.Join(f.source, "command-lists")); err != nil {
		t.Fatalf("installer did not keep built executable: %v", err)
	}
}

func TestLifecycleInstallRunsExistingUninstallerBeforeLink(t *testing.T) {
	f := newLifecycleFixture(t)
	oldSource := filepath.Join(f.base, "old-plugin-source")
	if err := os.MkdirAll(oldSource, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"herdr.command-lists\"\n"
	if err := os.WriteFile(filepath.Join(oldSource, "herdr-plugin.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	uninstaller := `#!/bin/sh
set -eu
printf 'old uninstall\n' >> "$LIFECYCLE_LOG"
rm -rf "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
`
	if err := os.WriteFile(filepath.Join(oldSource, "uninstall.sh"), []byte(uninstaller), 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := runLifecycleScript(t, f, "install.sh", "LIFECYCLE_INSTALLED_ROOT="+oldSource)
	if err != nil {
		t.Fatalf("replacement install failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(oldSource); !os.IsNotExist(err) {
		t.Fatalf("old source still exists: %v", err)
	}
	calls, _ := os.ReadFile(f.log)
	got := string(calls)
	if !strings.Contains(got, "helper installed-root\nold uninstall\nherdr plugin link .\n") {
		t.Fatalf("existing uninstall did not run before link:\n%s", got)
	}
}

func TestLifecycleInstallReplacesSameSourceWithoutDeletingIt(t *testing.T) {
	f := newLifecycleFixture(t)
	helperData, err := os.ReadFile(f.helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.source, "command-lists"), helperData, 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := runLifecycleScript(t, f, "install.sh", "LIFECYCLE_INSTALLED_ROOT="+f.source)
	if err != nil {
		t.Fatalf("same-source reinstall failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("same-source reinstall deleted source: %v", err)
	}
	calls, _ := os.ReadFile(f.log)
	got := string(calls)
	if !strings.Contains(got, "herdr plugin unlink herdr.command-lists\n") || !strings.Contains(got, "herdr plugin link .\n") {
		t.Fatalf("same-source reinstall did not unlink then link:\n%s", got)
	}
}

func TestLifecycleUninstallKeepsOnlyListsAndDeletesSource(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := os.MkdirAll(filepath.Join(f.config, "lists"), 0o700); err != nil {
		t.Fatal(err)
	}
	personal := filepath.Join(f.config, "lists", "Personal.md")
	if err := os.WriteFile(personal, []byte("whoami\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(f.config, "HELP.md"), filepath.Join(f.config, ".generated"), filepath.Join(f.config, "other", "state")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("remove\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	helperData, err := os.ReadFile(f.helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.source, "command-lists"), helperData, 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := runLifecycleScript(t, f, "uninstall.sh")
	if err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(f.source); !os.IsNotExist(err) {
		t.Fatalf("source directory still exists: %v", err)
	}
	data, err := os.ReadFile(personal)
	if err != nil || string(data) != "whoami\n" {
		t.Fatalf("personal list was lost or changed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(f.config)
	if err != nil || len(entries) != 1 || entries[0].Name() != "lists" {
		t.Fatalf("plugin config should contain only lists/: %v, %v", entries, err)
	}
	calls, _ := os.ReadFile(f.log)
	want := "herdr plugin config-dir herdr.command-lists\nhelper shortcut-config remove\nherdr plugin unlink herdr.command-lists\nherdr server reload-config\n"
	if string(calls) != want {
		t.Fatalf("unexpected uninstall actions:\n%s", calls)
	}
	if !strings.Contains(output, filepath.Join(f.config, "lists")) {
		t.Fatalf("uninstaller did not report retained lists path: %s", output)
	}
}

func TestLifecycleBuildFailuresKeepWorkingBinary(t *testing.T) {
	for _, fail := range []string{"test", "build"} {
		t.Run(fail, func(t *testing.T) {
			f := newLifecycleFixture(t)
			path := filepath.Join(f.source, "command-lists")
			if err := os.WriteFile(path, []byte("old working binary"), 0o700); err != nil {
				t.Fatal(err)
			}
			output, err := runLifecycleScript(t, f, "install.sh", "LIFECYCLE_FAIL="+fail)
			if err == nil {
				t.Fatalf("expected %s failure: %s", fail, output)
			}
			contents, _ := os.ReadFile(path)
			calls, _ := os.ReadFile(f.log)
			leftovers, _ := filepath.Glob(filepath.Join(f.source, ".command-lists-build.*"))
			if string(contents) != "old working binary" || strings.Contains(string(calls), "herdr") || len(leftovers) != 0 {
				t.Fatalf("failed build changed installed state: %s; %s; %v", contents, calls, leftovers)
			}
		})
	}
}

func TestLifecycleUninstallRefusesToDeleteAfterConfigFailure(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := os.MkdirAll(filepath.Join(f.config, "lists"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.config, "HELP.md"), []byte("keep on failure\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	helperData, _ := os.ReadFile(f.helper)
	if err := os.WriteFile(filepath.Join(f.source, "command-lists"), helperData, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := runLifecycleScript(t, f, "uninstall.sh", "LIFECYCLE_FAIL=shortcut")
	if err == nil {
		t.Fatalf("uninstall ignored config failure: %s", output)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("source was deleted after config failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.config, "HELP.md")); err != nil {
		t.Fatalf("plugin config was cleaned after config failure: %v", err)
	}
	calls, _ := os.ReadFile(f.log)
	want := "herdr plugin config-dir herdr.command-lists\nhelper shortcut-config remove\n"
	if string(calls) != want {
		t.Fatalf("unsafe action after config failure: %s", calls)
	}
}

func TestLifecycleInstallRefusesExecutableSymlink(t *testing.T) {
	f := newLifecycleFixture(t)
	if err := os.Symlink(f.helper, filepath.Join(f.source, "command-lists")); err != nil {
		t.Fatal(err)
	}
	if output, err := runLifecycleScript(t, f, "install.sh"); err == nil {
		t.Fatalf("install accepted a symlink: %s", output)
	}
	if _, err := os.Stat(f.log); !os.IsNotExist(err) {
		t.Fatal("installer performed actions before rejecting a symlink")
	}
}

func TestLifecycleUninstallRefusesLookalikeConfigPath(t *testing.T) {
	f := newLifecycleFixture(t)
	unsafe := filepath.Join(f.base, "important", pluginID)
	if err := os.MkdirAll(unsafe, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(unsafe, "DO-NOT-DELETE")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	helperData, err := os.ReadFile(f.helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.source, "command-lists"), helperData, 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := runLifecycleScript(t, f, "uninstall.sh", "LIFECYCLE_CONFIG="+unsafe)
	if err == nil {
		t.Fatalf("uninstall accepted lookalike config path: %s", output)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("unsafe config content was touched: %v", err)
	}
	if _, err := os.Stat(f.source); err != nil {
		t.Fatalf("source was touched after config-path refusal: %v", err)
	}
	calls, _ := os.ReadFile(f.log)
	if string(calls) != "herdr plugin config-dir herdr.command-lists\n" {
		t.Fatalf("uninstaller performed actions after unsafe config result: %s", calls)
	}
}

func TestLifecycleUninstallKeepsSourceOutsideSafeRoot(t *testing.T) {
	f := newLifecycleFixture(t)
	outside := filepath.Join(f.base, "outside-source")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uninstall.sh", "herdr-plugin.toml"} {
		data, err := os.ReadFile(filepath.Join(f.source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, name), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	helperData, err := os.ReadFile(f.helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "command-lists"), helperData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.config, "lists"), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", filepath.Join(outside, "uninstall.sh"))
	cmd.Dir = outside
	cmd.Env = append([]string{}, f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("uninstall outside safe root failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("source outside safe root was deleted: %v", err)
	}
	if !strings.Contains(string(output), "Source kept for safety") {
		t.Fatalf("missing safe-source notice: %s", output)
	}
}

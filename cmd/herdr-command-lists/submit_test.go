package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Record argv, not shell text: quoting and call order are part of the contract.
func recordHerdrCalls(t *testing.T, failClear bool) func() [][]string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	fakeHerdr := filepath.Join(dir, "herdr")
	script := "#!/bin/sh\nprintf '%s\\000' \"$#\" \"$@\" >> \"$HERDR_TEST_CALLS\"\n"
	if failClear {
		script += "if [ \"$1\" = pane ] && [ \"$2\" = send-keys ]; then exit 9; fi\n"
	}
	if err := os.WriteFile(fakeHerdr, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_BIN_PATH", fakeHerdr)
	t.Setenv("HERDR_TEST_CALLS", logPath)
	return func() [][]string {
		t.Helper()
		data, err := os.ReadFile(logPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
		var calls [][]string
		for len(parts) > 0 {
			n, err := strconv.Atoi(parts[0])
			if err != nil || n < 0 || n >= len(parts) {
				t.Fatalf("invalid recorded argv: %#v", parts)
			}
			calls = append(calls, parts[1:n+1])
			parts = parts[n+1:]
		}
		return calls
	}
}

func TestSubmitClearsWholeInputBeforeRunningExactCommand(t *testing.T) {
	calls := recordHerdrCalls(t, false)
	const pane = "w3:p2"
	const command = `printf '%s\n' 'a b' "$HOME"; printf 'done\n'`
	if err := submitCommand(pane, command); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"pane", "send-keys", pane, "ctrl+e", "ctrl+u"},
		{"pane", "run", pane, command},
	}
	if got := calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Herdr calls = %#v, want %#v", got, want)
	}
}

func TestSubmitStopsIfClearingInputFails(t *testing.T) {
	calls := recordHerdrCalls(t, true)
	if err := submitCommand("w1:p4", "clear"); err == nil {
		t.Fatal("expected clearing failure")
	}
	want := [][]string{{"pane", "send-keys", "w1:p4", "ctrl+e", "ctrl+u"}}
	if got := calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("a failed clear must not run the command: %#v", got)
	}
}

func TestSubmitRejectsUnsafeInputBeforeAnyPaneChanges(t *testing.T) {
	calls := recordHerdrCalls(t, false)
	for _, command := range []string{"date\nwhoami", "echo \x1b[31m", "echo \u202Ehidden", string([]byte{0xff})} {
		if err := submitCommand("w1:p1", command); err == nil {
			t.Fatalf("expected %q to be rejected", command)
		}
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("unsafe input changed the target pane: %#v", got)
	}
}

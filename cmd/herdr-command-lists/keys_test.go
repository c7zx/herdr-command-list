package main

import (
	"errors"
	"strings"
	"testing"
)

func keyBytes(input string) byteReader {
	data := []byte(input)
	return func() (byte, bool, error) {
		if len(data) == 0 {
			return 0, false, nil
		}
		b := data[0]
		data = data[1:]
		return b, true, nil
	}
}

func keyChunks(chunks ...string) byteReader {
	return func() (byte, bool, error) {
		if len(chunks) == 0 {
			return 0, false, nil
		}
		if len(chunks[0]) == 0 {
			chunks = chunks[1:]
			return 0, false, nil
		}
		b := chunks[0][0]
		chunks[0] = chunks[0][1:]
		return b, true, nil
	}
}

func TestKeyDecoderSupportsPopupNavigation(t *testing.T) {
	for _, tc := range []struct {
		input, event string
		r            rune
	}{
		{"\x1b[A", "up", 0}, {"\x1bOA", "up", 0},
		{"\x1b[B", "down", 0}, {"\x1bOB", "down", 0},
		{"\x1b[C", "right", 0}, {"\x1b[D", "left", 0},
		{"\x00", "ctrl+space", 0}, {"\x03", "ctrl+c", 0}, {"\x15", "ctrl+u", 0},
		{"\r", "enter", 0}, {"\n", "enter", 0}, {"\x7f", "backspace", 0},
		{"\x1b", "escape", 0}, {"?", "help", 0},
		{"a", "rune", 'a'}, {"A", "rune", 'A'}, {"ä", "rune", 'ä'}, {"✓", "rune", '✓'},
	} {
		t.Run(tc.event+"/"+tc.input, func(t *testing.T) {
			event, r, err := readKeyFrom(keyBytes(tc.input))
			if err != nil || event != tc.event || r != tc.r {
				t.Fatalf("decode(%q) = %q/%q/%v, want %q/%q", tc.input, event, r, err, tc.event, tc.r)
			}
		})
	}
}

func TestUnusedTabAndModifiedLetterKeysAreIgnored(t *testing.T) {
	for _, input := range []string{"\t", "\x1b[Z", "\x1ba", "\x1bd", "\x1b[97;9u", "\x1b[100;9u"} {
		if event, r, err := readKeyFrom(keyBytes(input)); err != nil || event != "" || r != 0 {
			t.Fatalf("unused key %q produced %q/%q/%v", input, event, r, err)
		}
	}
}

func TestKeyDecoderConsumesUnsupportedSequencesWithoutTurningThemIntoInput(t *testing.T) {
	for _, sequence := range []string{
		"\x1b[99~",
		"\x1b[97;9:3u",
		"\x1b[1114112u",
		"\x1b[4294967309u",
		"\x1b[" + strings.Repeat("1", 70) + "~",
	} {
		next := keyBytes(sequence + "z")
		if event, r, err := readKeyFrom(next); err != nil || event != "" || r != 0 {
			t.Fatalf("unsupported sequence %q produced %q/%q/%v", sequence, event, r, err)
		}
		if event, r, err := readKeyFrom(next); err != nil || event != "rune" || r != 'z' {
			t.Fatalf("sequence %q left trailing input or consumed next key: %q/%q/%v", sequence, event, r, err)
		}
	}
	for _, input := range []string{"\x1b[", "\x1b[97;", "\xc3", "\xff"} {
		if event, _, err := readKeyFrom(keyBytes(input)); err != nil || event != "" {
			t.Fatalf("incomplete/invalid input %q became an event %q: %v", input, event, err)
		}
	}
	wantErr := errors.New("read failed")
	if _, _, err := readKeyFrom(func() (byte, bool, error) { return 0, false, wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("input error was swallowed: %v", err)
	}
}

func TestBracketedPasteProducesNoSearchOrActionsUntilExplicitKey(t *testing.T) {
	paste := "\x1b[200~whoami\n\r\x03\x15\x00\t?\x1b[A\x1b[C\x1b[201~"
	reader := keyReader{next: keyBytes(paste + "\r")}
	if event, r, err := reader.read(); err != nil || event != "" || r != 0 {
		t.Fatalf("bracketed paste produced a search/action event: %q/%q/%v", event, r, err)
	}
	if event, r, err := reader.read(); err != nil || event != "enter" || r != 0 {
		t.Fatalf("explicit Enter after the paste was not preserved: %q/%q/%v", event, r, err)
	}
}

func TestBracketedPasteDiscardSurvivesTimeoutAndSplitEndMarker(t *testing.T) {
	reader := keyReader{next: keyChunks("\x1b[200~whoami", "\n\x1b[A\x1b[20", "1~\r")}
	for read := 0; read < 3; read++ {
		if event, r, err := reader.read(); err != nil || event != "" || r != 0 {
			t.Fatalf("paste read %d leaked input after a timeout: %q/%q/%v", read+1, event, r, err)
		}
	}
	if event, r, err := reader.read(); err != nil || event != "enter" || r != 0 {
		t.Fatalf("explicit Enter after the split paste end was not preserved: %q/%q/%v", event, r, err)
	}
}

func TestBracketedPasteStartMarkerSurvivesTimeout(t *testing.T) {
	reader := keyReader{next: keyChunks("\x1b[20", "0~whoami\n\x1b[201~\r")}
	for read := 0; read < 2; read++ {
		if event, r, err := reader.read(); err != nil || event != "" || r != 0 {
			t.Fatalf("split paste start leaked input on read %d: %q/%q/%v", read+1, event, r, err)
		}
	}
	if event, r, err := reader.read(); err != nil || event != "enter" || r != 0 {
		t.Fatalf("explicit Enter after split paste start was not preserved: %q/%q/%v", event, r, err)
	}
}

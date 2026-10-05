package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var terminalEscape = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

func plainScreen(s string) []string {
	return strings.Split(terminalEscape.ReplaceAllString(s, ""), "\r\n")
}

func TestRenderedViewportStaysInPlaceWhileMovingUp(t *testing.T) {
	m := &uiModel{selected: -1}
	for i := 0; i < 20; i++ {
		m.entries = append(m.entries, listEntry{kind: entryCommand, text: fmt.Sprintf("command-%02d", i)})
	}
	m.lists = []commandList{{name: "Long", entries: m.entries}}
	m.refresh(false)
	for i := 0; i < 9; i++ {
		m.down()
		render(m, 10, 60)
	}
	for selected := 7; selected >= 4; selected-- {
		m.up()
		lines := plainScreen(render(m, 10, 60))
		for row := 0; row < 5; row++ {
			prefix := "  "
			if row+4 == selected {
				prefix = "> "
			}
			want := fmt.Sprintf("%scommand-%02d", prefix, row+4)
			if got := lines[row+4]; got != want {
				t.Fatalf("selection %d moved screen row %d: %q, want %q", selected, row, got, want)
			}
		}
	}
	m.up()
	lines := plainScreen(render(m, 10, 60))
	if lines[4] != "> command-03" {
		t.Fatalf("moving beyond top did not reveal previous command: %#v", lines)
	}
}

func TestRenderFitsSmallPopupAndKeepsLongActiveTabVisible(t *testing.T) {
	lists := []commandList{
		{name: "First"},
		{name: "Second"},
		{name: "This is a very long active list name"},
		{name: "Fourth"},
		{name: "Fifth"},
	}
	for _, size := range [][2]int{{6, 12}, {10, 32}, {24, 80}, {2, 7}} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/empty=%v", size[0], size[1], empty), func(t *testing.T) {
				m := &uiModel{lists: lists, activeTab: 2, selected: -1}
				if !empty {
					m.entries = []listEntry{{kind: entryCommand, text: strings.Repeat("long command ", 30)}}
				}
				m.refresh(false)
				lines := plainScreen(render(m, size[0], size[1]))
				if len(lines) > size[0] {
					t.Fatalf("render uses %d rows in a %d-row popup", len(lines), size[0])
				}
				for i, line := range lines {
					if width := utf8.RuneCountInString(line); width > size[1] {
						t.Fatalf("row %d has width %d > %d: %q", i, width, size[1], line)
					}
				}
				if size[0] >= 6 && size[1] >= 12 && (!strings.Contains(lines[1], "[This") || !strings.Contains(lines[1], "…]")) {
					t.Fatalf("long active tab is missing or not shortened: %q", lines[1])
				}
			})
		}
	}
}

func TestTruncateRespectsWideAndCombiningCharacters(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  string
	}{
		{"漢字abc", 5, "漢字…"},
		{"漢字", 3, "漢…"},
		{"e\u0301cole", 3, "e\u0301c…"},
		{"date", 4, "date"},
		{"date", 1, "…"},
		{"date", 0, ""},
	} {
		if got := truncate(tc.text, tc.width); got != tc.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
}

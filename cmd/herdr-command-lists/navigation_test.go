package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestViewportMovesOnlyWhenSelectionLeavesVisibleRows(t *testing.T) {
	m := &uiModel{selected: -1}
	for i := 0; i < 20; i++ {
		m.entries = append(m.entries, listEntry{kind: entryCommand, text: fmt.Sprintf("command-%02d", i)})
	}
	m.refresh(false)
	const height = 5
	for i := 0; i < 9; i++ {
		m.down()
		m.ensureVisible(height)
	}
	if m.selected != 8 || m.offset != 4 {
		t.Fatalf("scrolled selection/offset = %d/%d, want 8/4", m.selected, m.offset)
	}
	for want := 7; want >= 4; want-- {
		m.up()
		m.ensureVisible(height)
		if m.selected != want || m.offset != 4 {
			t.Fatalf("Up inside viewport = %d/%d, want %d/4", m.selected, m.offset, want)
		}
	}
	m.up()
	m.ensureVisible(height)
	if m.selected != 3 || m.offset != 3 {
		t.Fatalf("Up above viewport = %d/%d, want 3/3", m.selected, m.offset)
	}
	for want := 4; want <= 7; want++ {
		m.down()
		m.ensureVisible(height)
		if m.selected != want || m.offset != 3 {
			t.Fatalf("Down inside viewport = %d/%d, want %d/3", m.selected, m.offset, want)
		}
	}
	m.down()
	m.ensureVisible(height)
	if m.selected != 8 || m.offset != 4 {
		t.Fatalf("Down below viewport = %d/%d, want 8/4", m.selected, m.offset)
	}

	m.selectEntry(0)
	m.ensureVisible(height)
	m.up()
	m.ensureVisible(height)
	if m.selected != 19 || m.offset != 15 {
		t.Fatalf("wrap to end = %d/%d, want 19/15", m.selected, m.offset)
	}
	m.down()
	m.ensureVisible(height)
	if m.selected != 0 || m.offset != 0 {
		t.Fatalf("wrap to start = %d/%d, want 0/0", m.selected, m.offset)
	}
}

func TestNavigationWrapsWithinFilteredCommands(t *testing.T) {
	m := &uiModel{entries: []listEntry{
		{kind: entryBlank},
		{kind: entryHeader, text: "# First"},
		{kind: entryCommand, text: "echo match-one"},
		{kind: entryCommand, text: "date"},
		{kind: entryBlank},
		{kind: entryHeader, text: "# Last"},
		{kind: entryCommand, text: "echo match-two"},
		{kind: entryBlank},
		{kind: entryHeader, text: "# Empty"},
	}, query: []rune("match"), selected: -1}
	m.refresh(false)
	for _, want := range []string{"echo match-one", "echo match-two", "echo match-one"} {
		m.down()
		if got, ok := m.current(); !ok || got != want {
			t.Fatalf("filtered Down = %q/%v, want %q", got, ok, want)
		}
	}
	m.up()
	if got, ok := m.current(); !ok || got != "echo match-two" {
		t.Fatalf("filtered Up wrap = %q/%v", got, ok)
	}
	m.query = []rune("not-present")
	m.refresh(true)
	for i := 0; i < 3; i++ {
		m.up()
		m.down()
		m.ensureVisible(5)
		if got, ok := m.current(); ok || got != "" || m.selected != -1 || m.offset != 0 {
			t.Fatalf("empty filter acquired selection: %q/%v, %d/%d", got, ok, m.selected, m.offset)
		}
	}
}

func TestNavigationWithNoCommandsOrOneCommand(t *testing.T) {
	for _, entries := range [][]listEntry{nil, {{kind: entryHeader, text: "# Empty"}, {kind: entryBlank}}} {
		m := &uiModel{entries: entries, selected: -1}
		m.refresh(false)
		m.up()
		m.down()
		if _, ok := m.current(); ok || m.selected != -1 {
			t.Fatalf("non-command rows became selectable: %#v", m)
		}
	}
	m := &uiModel{entries: []listEntry{{kind: entryHeader, text: "# Only"}, {kind: entryCommand, text: "date"}, {kind: entryBlank}}, selected: -1}
	m.refresh(false)
	for i := 0; i < 3; i++ {
		m.up()
		m.down()
		if got, ok := m.current(); !ok || got != "date" {
			t.Fatalf("one-command wrap = %q/%v", got, ok)
		}
	}
}

func TestSwitchTabsWrapsAndResetsSearchSelectionAndViewport(t *testing.T) {
	lists := []commandList{
		{name: "First", path: "/lists/First.md", entries: []listEntry{{kind: entryCommand, text: "whoami"}}},
		{name: "Second", path: "/lists/Second.md", entries: []listEntry{{kind: entryHeader, text: "# Other"}, {kind: entryCommand, text: "date"}}},
		{name: "Third", path: "/lists/Third.md", entries: nil},
	}
	m := &uiModel{lists: lists, entries: lists[0].entries, query: []rune("who"), selected: -1}
	m.refresh(true)
	m.offset = 9
	m.switchTab(1)
	if m.activeTab != 1 || len(m.query) != 0 || m.selected != -1 || m.offset != 0 {
		t.Fatalf("new tab retained old UI state: tab=%d query=%q selected=%d offset=%d", m.activeTab, m.query, m.selected, m.offset)
	}
	if !reflect.DeepEqual(m.entries, lists[1].entries) {
		t.Fatalf("new tab shows incorrect entries: %#v", m.entries)
	}
	m.down()
	if got, ok := m.current(); !ok || got != "date" {
		t.Fatalf("new tab selection = %q/%v", got, ok)
	}
	m.switchTab(-1)
	m.switchTab(-1)
	if m.activeTab != 2 || len(m.entries) != 0 {
		t.Fatalf("previous tab did not wrap to empty third list: %#v", m)
	}
	m.switchTab(1)
	if m.activeTab != 0 || !reflect.DeepEqual(m.entries, lists[0].entries) {
		t.Fatalf("next tab did not wrap to first list: %#v", m)
	}
	(&uiModel{selected: -1}).switchTab(1) // an empty lists directory must be safe
}

package main

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const (
	listsDirName = "lists"
	helpFile     = "HELP.md"
	maxFileBytes = 1 << 20
	maxLists     = 128
)

//go:embed assets/HELP.md
var embeddedHelp []byte

//go:embed assets/examples/*.md
var embeddedExamples embed.FS

type commandList struct {
	name    string
	path    string
	entries []listEntry
}

func ensureListsDir() (string, error) {
	dir, err := pluginConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := privateDir(dir); err != nil {
		return "", err
	}
	if _, err := ensureHelpPath(dir); err != nil {
		return "", err
	}

	path := filepath.Join(dir, listsDirName)
	if _, err := os.Lstat(path); err == nil {
		if err := privateDir(path); err != nil {
			return "", err
		}
		hasLists, err := hasListFiles(path)
		if err != nil {
			return "", err
		}
		if hasLists {
			return path, nil
		}
		if err := seedExamples(path); err != nil {
			return "", err
		}
		return path, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}

	// Publish a complete starter directory atomically on first install.
	stage, err := os.MkdirTemp(dir, ".lists-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := seedExamples(stage); err != nil {
		return "", err
	}
	if err := os.Rename(stage, path); err != nil {
		// A concurrent opener may already have initialized the directory.
		if checkErr := privateDir(path); checkErr != nil {
			return "", err
		}
	}
	return path, privateDir(path)
}

func hasListFiles(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".md" {
			return true, nil
		}
	}
	return false, nil
}

func seedExamples(path string) error {
	examples, err := embeddedExamples.ReadDir("assets/examples")
	if err != nil {
		return err
	}
	for _, example := range examples {
		data, err := embeddedExamples.ReadFile("assets/examples/" + example.Name())
		if err != nil {
			return err
		}
		if err := createPrivateFile(filepath.Join(path, example.Name()), data); err != nil {
			return err
		}
	}
	return nil
}

func privateDir(path string) error {
	if err := checkPath(path, true); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q must be a directory", path)
	}
	return f.Chmod(0o700)
}

func createPrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return checkPath(path, false)
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func ensureHelpPath(dir string) (string, error) {
	path := filepath.Join(dir, helpFile)
	data, err := readRegularFile(path)
	if err == nil && bytes.Equal(data, embeddedHelp) {
		f, err := openCommandFile(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		return path, f.Chmod(0o600)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	// HELP.md is generated documentation, replaced atomically on installation.
	f, err := os.CreateTemp(dir, ".help-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(embeddedHelp)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func checkPath(path string, wantDir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q must not be a symbolic link", path)
	}
	if wantDir && !info.IsDir() {
		return fmt.Errorf("%q must be a directory", path)
	}
	if !wantDir && !info.Mode().IsRegular() {
		return fmt.Errorf("%q must be a regular file", path)
	}
	return nil
}

func openCommandFile(path string) (*os.File, error) {
	if err := checkPath(path, false); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	// Do not follow a swapped symlink or block on a swapped FIFO after Lstat.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, fmt.Errorf("%q changed while opening", path)
	}
	return f, nil
}

func readRegularFile(path string) ([]byte, error) {
	f, err := openCommandFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, fmt.Errorf("%q exceeds the 1 MiB file limit", path)
	}
	return data, nil
}

func loadHelpLines(path string) ([]string, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n"), nil
}

func loadLists(dir string) ([]commandList, error) {
	if err := checkPath(dir, true); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir) // stable filename order, without shell globbing
	if err != nil {
		return nil, err
	}
	var lists []commandList
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".md" {
			continue
		}
		if len(lists) >= maxLists {
			return nil, fmt.Errorf("at most %d command lists are supported", maxLists)
		}
		name := strings.TrimSuffix(file.Name(), ".md")
		if err := validateVisibleLine(name); err != nil {
			return nil, fmt.Errorf("list filename %q: %w", file.Name(), err)
		}
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("a list filename must have a non-empty name before .md")
		}
		path := filepath.Join(dir, file.Name())
		entries, err := loadEntries(path)
		if err != nil {
			return nil, err
		}
		lists = append(lists, commandList{name: name, path: path, entries: entries})
	}
	// Keep bundled starter tabs in their intended order. Any additional lists
	// retain their filename order after the bundled tabs.
	starterOrder := []string{"Main", "Systems", "More"}
	ordered := make([]commandList, 0, len(lists))
	used := make([]bool, len(lists))
	for _, starter := range starterOrder {
		for i, list := range lists {
			if list.name == starter {
				ordered = append(ordered, list)
				used[i] = true
				break
			}
		}
	}
	for i, list := range lists {
		if !used[i] {
			ordered = append(ordered, list)
		}
	}
	lists = ordered
	return lists, nil
}

func loadEntries(path string) ([]listEntry, error) {
	data, err := readRegularFile(path)
	if err != nil {
		return nil, err
	}
	return parseEntries(data, path)
}

func parseEntries(data []byte, path string) ([]listEntry, error) {
	var entries []listEntry
	s := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; s.Scan(); lineNo++ {
		line := s.Text()
		if err := validateVisibleLine(line); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", filepath.Base(path), lineNo, err)
		}
		trimmed := strings.TrimSpace(line)
		kind := entryCommand
		if trimmed == "" {
			kind = entryBlank
		} else if strings.HasPrefix(trimmed, "#") {
			kind = entryHeader
		}
		if kind == entryBlank {
			line = ""
		}
		entries = append(entries, listEntry{kind: kind, text: line})
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return entries, nil
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
	if err := validateVisibleLine(command); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return errors.New("select a non-empty command")
	}
	return nil
}

func safeDisplay(s string) string {
	return strings.Map(func(r rune) rune {
		if unsafeRune(r) {
			return '�'
		}
		return r
	}, s)
}

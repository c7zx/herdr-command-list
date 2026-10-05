package main

// This is a deliberately narrow TOML editor for the install/uninstall scripts.
// It never reserializes the user's config. Unusual syntax fails unchanged so a
// user can configure the shortcut manually without a third-party TOML library.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var commandTableHeader = regexp.MustCompile(`^\[\[\s*(keys|"keys"|'keys')\s*\.\s*(command|"command"|'command')\s*\]\]$`)
var commandSubtableHeader = regexp.MustCompile(`^\[\[?\s*(keys|"keys"|'keys')\s*\.\s*(command|"command"|'command')\s*\.`)
var keysTableHeader = regexp.MustCompile(`^\[\s*(keys|"keys"|'keys')\s*\]$`)
var commandFieldName = regexp.MustCompile(`^(keys|"keys"|'keys')\s*\.\s*(command|"command"|'command')$`)
var nativeKeysHeader = regexp.MustCompile(`^\[\s*(keys|"keys"|'keys')(\s*\.|\s*\])`)
var nativeKeyField = regexp.MustCompile(`^(keys|"keys"|'keys')\s*\.`)

type shortcutTable struct {
	start, end int
	fields     map[string]string
	nextHeader string
}

func configureShortcut(mode, path string) (string, error) {
	if mode != "install" && mode != "remove" {
		return "", errors.New("shortcut-config mode must be install or remove")
	}
	original, info, err := readShortcutFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if os.IsNotExist(err) && mode == "remove" {
		return "No plugin shortcuts to remove.", nil
	}
	next, status, err := editShortcutConfig(string(original), mode)
	if err != nil {
		return "", fmt.Errorf("shortcut config left unchanged: %w", err)
	}
	if next == string(original) {
		return status, nil
	}
	backup, err := saveShortcutConfig(path, original, []byte(next), info)
	if err != nil {
		return "", err
	}
	if backup != "" {
		status += "\nConfig backup: " + backup
	}
	return status, nil
}

func readShortcutFile(path string) ([]byte, os.FileInfo, error) {
	f, err := openCommandFile(path) // rejects symlinks, special files and swaps
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > 2<<20 {
		return nil, nil, errors.New("config exceeds the 2 MiB shortcut-editing limit; configure the shortcut manually")
	}
	return data, info, nil
}

func editShortcutConfig(source, mode string) (string, string, error) {
	lines := strings.SplitAfter(source, "\n")
	tables := []shortcutTable{}
	current, depth, ctrlYUsed := -1, 0, false
	keysTable, nativeKeys, keyAmbiguous := false, false, false
	for i, line := range lines {
		code, delta, err := shortcutTOMLLine(line)
		if err != nil {
			return "", "", fmt.Errorf("line %d: %w", i+1, err)
		}
		trimmed := strings.TrimSpace(code)
		if depth == 0 && strings.HasPrefix(trimmed, "[") {
			if strings.HasSuffix(trimmed, "]") && commandFieldName.MatchString(strings.TrimSpace(trimmed[1:len(trimmed)-1])) {
				return "", "", errors.New("shortcut actions must use [[keys.command]] array tables")
			}
			if current >= 0 {
				tables[current].end, tables[current].nextHeader = i, trimmed
			}
			current = -1
			keysTable = keysTableHeader.MatchString(trimmed)
			nativeKeys = nativeKeysHeader.MatchString(trimmed)
			if commandTableHeader.MatchString(trimmed) {
				tables = append(tables, shortcutTable{start: i, end: len(lines), fields: map[string]string{}})
				current = len(tables) - 1
			}
			continue
		}
		if depth == 0 {
			name, value, found := strings.Cut(code, "=")
			name = strings.TrimSpace(name)
			if found && (nativeKeys || nativeKeyField.MatchString(name)) {
				parsed, err := shortcutString(strings.TrimSpace(value))
				if err != nil {
					keyAmbiguous = true
				} else if shortcutIsCtrlY(parsed) {
					ctrlYUsed = true
				}
			}
			if found && (commandFieldName.MatchString(name) || (keysTable && name == "command")) {
				return "", "", errors.New("inline or dotted shortcut arrays need manual shortcut configuration")
			}
			if strings.HasPrefix(name, "\"") || strings.HasPrefix(name, "'") {
				name, _ = shortcutString(name)
			}
			if found && (name == "keys" || (keysTable && name == "command")) {
				return "", "", errors.New("inline shortcut tables need manual shortcut configuration")
			}
			if found && (name == "key" || (current >= 0 && (name == "type" || name == "command" || name == "description"))) {
				value = strings.TrimSpace(value)
				parsed, err := shortcutString(value)
				if err != nil {
					return "", "", fmt.Errorf("line %d: shortcut fields must use a single-line string", i+1)
				}
				if name == "key" && shortcutIsCtrlY(parsed) {
					ctrlYUsed = true
				}
				if current >= 0 {
					if _, exists := tables[current].fields[name]; exists {
						return "", "", fmt.Errorf("line %d: duplicate shortcut field %s", i+1, name)
					}
					tables[current].fields[name] = parsed
				}
			}
		}
		depth += delta
		if depth < 0 {
			return "", "", fmt.Errorf("line %d: unbalanced TOML brackets", i+1)
		}
	}
	if depth != 0 {
		return "", "", errors.New("unbalanced TOML brackets")
	}
	owned := 0
	for _, table := range tables {
		action := table.fields["command"]
		if table.fields["type"] != "plugin_action" || action != pluginID+".toggle" {
			continue
		}
		if commandSubtableHeader.MatchString(table.nextHeader) {
			return "", "", errors.New("plugin shortcut has a nested table; edit that shortcut manually")
		}
		owned++
		if mode == "remove" {
			for i := table.start; i < table.end; i++ {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
					lines[i] = ""
				}
			}
		}
	}
	if mode == "remove" {
		return strings.Join(lines, ""), fmt.Sprintf("Removed %d plugin shortcut(s).", owned), nil
	}
	if owned > 0 {
		return strings.Join(lines, ""), "Existing plugin shortcut preserved.", nil
	}
	if ctrlYUsed {
		return source, "Ctrl+Y is already in use; no shortcut added. Add a free key for herdr.command-lists.toggle manually.", nil
	}
	if keyAmbiguous {
		return source, "No shortcut added: the existing key syntax needs a manual Ctrl+Y conflict check.", nil
	}
	separator := "\n"
	if source != "" && !strings.HasSuffix(source, "\n") {
		separator = "\n\n"
	}
	return source + separator + "[[keys.command]]\nkey = \"ctrl+y\"\ntype = \"plugin_action\"\ncommand = \"" + pluginID + ".toggle\"\ndescription = \"Command Lists\"\n", "Shortcut: Ctrl+Y.", nil
}

func shortcutIsCtrlY(key string) bool {
	key = strings.ToLower(strings.Join(strings.Fields(key), ""))
	return key == "ctrl+y" || key == "control+y"
}

func shortcutString(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return strconv.Unquote(value)
	}
	return "", errors.New("unsupported string")
}

// Ignore comments and bracket characters inside strings. Refuse multiline
// strings rather than risk treating their contents as real config tables.
func shortcutTOMLLine(line string) (string, int, error) {
	quote, delta := byte(0), 0
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '#':
			return line[:i], delta, nil
		case '\'', '"':
			if i+2 < len(line) && line[i+1] == c && line[i+2] == c {
				return "", 0, errors.New("multiline TOML strings need manual shortcut configuration")
			}
			quote = c
		case '[', '{':
			delta++
		case ']', '}':
			delta--
		}
	}
	if quote != 0 {
		return "", 0, errors.New("unterminated TOML string")
	}
	return line, delta, nil
}

func saveShortcutConfig(path string, original, next []byte, info os.FileInfo) (string, error) {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() {
		return "", fmt.Errorf("config parent %s must be a real directory", parent)
	}
	backup := ""
	if info != nil {
		file, err := os.CreateTemp(parent, filepath.Base(path)+".command-lists-backup-*")
		if err != nil {
			return "", err
		}
		backup = file.Name()
		_, writeErr := file.Write(original)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return "", errors.Join(writeErr, closeErr)
		}
	}
	file, err := os.CreateTemp(parent, ".command-lists-config-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if info != nil {
		err = file.Chmod(info.Mode().Perm())
	}
	if err == nil {
		_, err = file.Write(next)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return "", errors.Join(err, closeErr)
	}
	// Detect concurrent edits or path replacement before replacing the file.
	if info == nil {
		_, statErr := os.Lstat(path)
		if !os.IsNotExist(statErr) {
			return "", errors.New("config appeared during setup; retry the installer")
		}
	} else {
		contents, current, readErr := readShortcutFile(path)
		if readErr != nil || !os.SameFile(info, current) {
			return "", errors.New("config path changed during setup; retry the installer")
		}
		if !bytes.Equal(contents, original) {
			return "", errors.New("config changed during setup; retry the installer")
		}
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return "", err
	}
	return backup, nil
}

#!/bin/sh
set -eu
umask 077
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

command -v herdr >/dev/null 2>&1 || { echo "Error: herdr is not in PATH." >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed." >&2; exit 1; }

# Deliberately use only the locally installed Go toolchain. No auto-download.
if ! test_output=$(GOTOOLCHAIN=local go test ./... 2>&1); then
    printf '%s\n' "$test_output" >&2
    exit 1
fi
GOTOOLCHAIN=local go build -trimpath -o command-list .

herdr plugin link . >/dev/null

command_path=$(./command-list init)
config_dir=$(dirname -- "$command_path")
help_path="$config_dir/HELP.md"

# Keep local plugin help next to commands.md without following a symlink.
if [ -L "$help_path" ]; then
    echo "Error: $help_path must not be a symbolic link." >&2
    exit 1
fi
cp HELP.md "$help_path"
chmod 600 "$help_path"

config_file=${HERDR_CONFIG_PATH:-"$HOME/.config/herdr/config.toml"}
config_parent=$(dirname -- "$config_file")
mkdir -p "$config_parent"
if [ -L "$config_file" ]; then
    echo "Error: $config_file must not be a symbolic link." >&2
    exit 1
elif [ ! -e "$config_file" ]; then
    : > "$config_file"
elif [ ! -f "$config_file" ]; then
    echo "Error: $config_file must be a regular file." >&2
    exit 1
fi

shortcut_status="Ctrl+Y"
reload_note=""
config_changed=0

if grep -Eq '^[[:space:]]*key[[:space:]]*=[[:space:]]*"ctrl\+y"[[:space:]]*$' "$config_file"; then
    if grep -Fq 'herdr.command-list.toggle' "$config_file"; then
        shortcut_status="Ctrl+Y (configured)"
    else
        shortcut_status="Ctrl+Y not added (already in use)"
    fi
else
    cat >> "$config_file" <<'KEYBIND'

# Command List shortcut.
# Change "ctrl+y" to another Herdr key (for example "prefix+y") to customize it.
[[keys.command]]
key = "ctrl+y"
type = "plugin_action"
command = "herdr.command-list.toggle"
description = "Command List"
KEYBIND
    config_changed=1
fi

if [ "$config_changed" -eq 1 ]; then
    if ! herdr server reload-config >/dev/null 2>&1; then
        reload_note="  Run: herdr server reload-config"
    fi
fi

printf '\nCommand List v1.0.1 installed successfully.\n\n'
printf 'Commands:\n  %s\n\n' "$command_path"
printf 'Shortcut:\n  %s\n  Config: %s\n' "$shortcut_status" "$config_file"
if [ -n "$reload_note" ]; then
    printf '%s\n' "$reload_note"
fi
printf '\nEdit commands:\n  nano %s\n\n' "$command_path"
printf '%s\n' 'Controls:'
printf '%s\n' '  ↑ / ↓       Select command'
printf '%s\n' '  Ctrl+Space  Next section'
printf '%s\n' '  Enter       Run'
printf '%s\n' '  ?           Help'
printf '%s\n\n' '  Esc/Ctrl+C  Close'
printf '%s\n' 'Open Command List with Ctrl+Y.'

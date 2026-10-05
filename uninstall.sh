#!/bin/sh
set -eu

plugin_id=herdr.command-lists
keep_source=0

case "$#:${1-}" in
    0:) ;;
    1:-h|1:--help)
        printf '%s\n' 'Usage: ./uninstall.sh  (remove Command Lists, but keep the lists/ directory)'
        exit 0
        ;;
    1:--keep-source)
        # Internal mode used by install.sh when reinstalling from the same source.
        keep_source=1
        ;;
    *)
        printf '%s\n' 'Usage: ./uninstall.sh' >&2
        exit 2
        ;;
esac

umask 077
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
manifest=$source_dir/herdr-plugin.toml
herdr_bin=${HERDR_BIN_PATH:-herdr}
helper=$source_dir/command-lists

fail() {
    printf 'Error: %s\n' "$*" >&2
    exit 1
}

valid_manifest() {
    [ -f "$1" ] && [ ! -L "$1" ] \
        && grep -Eq '^[[:space:]]*id[[:space:]]*=[[:space:]]*"herdr\.command-lists"[[:space:]]*$' "$1"
}

valid_manifest "$manifest" \
    || fail 'refusing to uninstall from a directory without the Command Lists manifest.'
case "$source_dir" in
    ''|/|"${HOME:-}") fail 'refusing an unsafe source path.' ;;
esac
command -v "$herdr_bin" >/dev/null 2>&1 \
    || fail 'Herdr is not in PATH (or HERDR_BIN_PATH).'
[ -f "$helper" ] && [ -x "$helper" ] && [ ! -L "$helper" ] \
    || fail 'the installed command-lists helper is missing or unsafe.'

config_dir=$("$herdr_bin" plugin config-dir "$plugin_id")
case "$config_dir" in
    /*) ;;
    *) fail "refusing a non-absolute plugin config path: $config_dir" ;;
esac

if [ -n "${HERDR_CONFIG_PATH:-}" ]; then
    case "$HERDR_CONFIG_PATH" in
        /*) config_root=$(dirname -- "$HERDR_CONFIG_PATH") ;;
        *) fail 'HERDR_CONFIG_PATH must be absolute for safe uninstall.' ;;
    esac
else
    [ -n "${HOME:-}" ] || fail 'HOME is not set.'
    config_root=$HOME/.config/herdr
fi
expected_dir=$config_root/plugins/config/$plugin_id

if [ -e "$config_dir" ] || [ -L "$config_dir" ]; then
    [ -d "$config_dir" ] && [ ! -L "$config_dir" ] \
        || fail "plugin config path is not a real directory: $config_dir"
    [ -d "$expected_dir" ] && [ ! -L "$expected_dir" ] \
        || fail "unexpected plugin config path: $config_dir"
    config_real=$(CDPATH= cd -- "$config_dir" && pwd -P)
    expected_real=$(CDPATH= cd -- "$expected_dir" && pwd -P)
    [ "$config_real" = "$expected_real" ] \
        || fail "refusing to clean unexpected plugin config path: $config_real"
    config_dir=$config_real
else
    [ "$config_dir" = "$expected_dir" ] \
        || fail "refusing unexpected missing plugin config path: $config_dir"
fi

# Remove the shortcut before unlinking so config-edit failures leave the plugin intact.
"$helper" shortcut-config remove >/dev/null
"$herdr_bin" plugin unlink "$plugin_id" >/dev/null

reload_note=''
if ! "$herdr_bin" server reload-config >/dev/null 2>&1; then
    reload_note='Restart Herdr or run: herdr server reload-config'
fi

# lists/ is user data. Remove only the other direct children of the verified config directory.
if [ -d "$config_dir" ]; then
    for item in "$config_dir"/* "$config_dir"/.[!.]* "$config_dir"/..?*; do
        [ -e "$item" ] || [ -L "$item" ] || continue
        [ "$item" = "$config_dir/lists" ] && continue
        rm -rf -- "$item"
    done
fi

source_note=''
if [ "$keep_source" -eq 0 ]; then
    [ -n "${HOME:-}" ] || fail 'HOME is not set.'
    source_root=$HOME/.herdr-plugins
    if [ -d "$source_root" ] && [ ! -L "$source_root" ]; then
        source_root=$(CDPATH= cd -- "$source_root" && pwd -P)
        case "$source_dir" in
            "$source_root"/*)
                valid_manifest "$manifest" \
                    || fail 'source manifest changed during uninstall; refusing recursive deletion.'
                source_parent=$(dirname -- "$source_dir")
                source_name=$(basename -- "$source_dir")
                cd "$source_parent"
                rm -rf -- "$source_name"
                ;;
            *) source_note="Source kept for safety (outside $HOME/.herdr-plugins): $source_dir" ;;
        esac
    else
        source_note="Source kept for safety: $source_dir"
    fi
fi

printf '\n%s\n' 'Command Lists uninstalled.'
printf 'Lists kept at: %s\n' "$config_dir/lists"
[ -z "$source_note" ] || printf '%s\n' "$source_note"
[ -z "$reload_note" ] || printf '%s\n' "$reload_note"

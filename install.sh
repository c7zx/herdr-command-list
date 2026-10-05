#!/bin/sh
set -eu
if [ "$#" -gt 0 ]; then
    printf '%s\n' 'Usage: ./install.sh  (test, replace an existing local install if present, then link this source)'
    case "$*" in -h|--help) exit 0 ;; *) exit 2 ;; esac
fi
umask 077
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
cd "$source_dir"

herdr_bin=${HERDR_BIN_PATH:-herdr}
command -v "$herdr_bin" >/dev/null 2>&1 || { echo "Error: Herdr is not in PATH (or HERDR_BIN_PATH)." >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed." >&2; exit 1; }

if [ -L command-lists ] || { [ -e command-lists ] && [ ! -f command-lists ]; }; then
    echo "Error: command-lists must be a regular file, not a symlink or directory." >&2
    exit 1
fi

# Prove the new source builds before removing any existing installation.
GOTOOLCHAIN=local go test ./...
build_path=$(mktemp "./.command-lists-build.XXXXXXXX")
trap 'rm -f "$build_path"' 0
trap 'exit 1' HUP INT TERM
GOTOOLCHAIN=local go build -trimpath -o "$build_path" ./cmd/herdr-command-lists

# Herdr's JSON plugin list is the source of truth for an existing local root.
# The freshly built helper parses it so paths with spaces/JSON escapes are safe.
installed_json=$("$herdr_bin" plugin list --plugin herdr.command-lists --json)
old_source=$(printf '%s' "$installed_json" | "$build_path" installed-root)
if [ -n "$old_source" ]; then
    if [ -L "$old_source" ] || [ ! -d "$old_source" ]; then
        echo "Error: registered Command Lists source is not a real directory: $old_source" >&2
        exit 1
    fi
    old_source=$(CDPATH= cd -- "$old_source" && pwd -P)
    old_manifest=$old_source/herdr-plugin.toml
    old_uninstaller=$old_source/uninstall.sh
    if [ ! -f "$old_manifest" ] || [ -L "$old_manifest" ] || ! grep -Eq '^[[:space:]]*id[[:space:]]*=[[:space:]]*"herdr\.command-lists"[[:space:]]*$' "$old_manifest"; then
        echo "Error: refusing to replace an installation with an unexpected manifest: $old_source" >&2
        exit 1
    fi
    if [ ! -f "$old_uninstaller" ] || [ -L "$old_uninstaller" ]; then
        echo "Error: existing Command Lists installation has no safe uninstall.sh: $old_source" >&2
        exit 1
    fi

    if [ "$old_source" = "$source_dir" ]; then
        # Reinstalling in place: unlink/clean generated config, but keep this source
        # alive long enough for the installer to continue.
        sh "$old_uninstaller" --keep-source >/dev/null
    else
        # Never let an old source tree delete the new release nested inside it.
        case "$source_dir/" in
            "$old_source/"*)
                echo 'Error: extract the new release outside the currently installed plugin directory.' >&2
                exit 1
                ;;
        esac
        # The old uninstaller preserves lists/. Hardened versions delete their source
        # only when it is below ~/.herdr-plugins; otherwise the old source is kept.
        sh "$old_uninstaller" >/dev/null
    fi
fi

mv -f "$build_path" command-lists

"$herdr_bin" plugin link . >/dev/null
lists_dir=$(./command-lists init)
./command-lists shortcut-config install

reload_note=""
if ! "$herdr_bin" server reload-config >/dev/null 2>&1; then
    reload_note="Restart Herdr or run: herdr server reload-config"
fi

printf '\nCommand Lists v1.0.2 installed.\n\n'
printf 'Lists: %s\n' "$lists_dir"
printf '%s\n' 'Edit the .md files in that directory; changes are read whenever the popup opens.'
printf '\n%s\n' 'Controls: ↑/↓ commands, ←/→ lists, Ctrl+Space next section, Enter run, ? help, Esc close.'
printf '%s\n' 'Uninstall from outside this source directory; your lists are kept.'
if [ -n "$reload_note" ]; then
    printf '\n%s\n' "$reload_note"
fi

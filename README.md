# Command List v1.0.1 — Herdr plugin for Linux & macOS

A small Herdr plugin that lets you browse and run commands from a command list stored in `commands.md`.

Plugin ID: `herdr.command-list`

## Preview

![Command List preview](preview/preview.png)

![Command List preview 2](preview/preview-2.png)

## Installation on Linux or macOS

Requirements:

- Herdr
- Go **1.23.2 or newer**. This is the compatibility minimum, not a pinned build version.
  Builds use the locally installed Go toolchain (`GOTOOLCHAIN=local`), so a system with Go 1.27 installed builds with Go 1.27.

```sh
herdr --version
go version
```

### Install from GitHub

Herdr can install the plugin from GitHub:

```sh
herdr plugin install c7zx/herdr-command-list
```

### Install from a local checkout

Extract the `.zip` archive into the directory where you want the plugin to remain installed.

From the extracted or cloned project directory:

```sh
./install.sh
```

The installer:

1. Runs the tests with the locally installed Go toolchain.
2. Builds `command-list`.
3. Links the plugin with Herdr.
4. Creates or verifies `commands.md`.
5. Copies `HELP.md` into the plugin config directory for local help.
6. Adds a `Ctrl+Y` Command List shortcut to `~/.config/herdr/config.toml` when that key is not already in use.

It uses no `sudo` and downloads nothing itself. The build sets `GOTOOLCHAIN=local`.

## commands.md

Show the plugin configuration directory:

```sh
herdr plugin config-dir herdr.command-list
```

Edit the command list:

```sh
nano "$(herdr plugin config-dir herdr.command-list)/commands.md"
```

A fresh installation starts with:

```text
# Command List Plugin
nano "$(herdr plugin config-dir herdr.command-list)/commands.md"
nano ~/.config/herdr/config.toml
cat "$(herdr plugin config-dir herdr.command-list)/HELP.md"
```

Rules:

- Every non-empty line that does not begin with `#` is an executable command.
- Lines whose first visible character is `#` are displayed as bold colored section headings and cannot be selected or executed.
- Blank lines are displayed as spacing.
- `commands.md` is re-read each time the popup opens, so editing it does not require rebuilding or restarting the plugin.

## Controls

- `Up`: select upward; from the initial empty selection it starts at the last command.
- `Down`: select downward; from the initial empty selection it starts at the first command.
- `Ctrl+Space`: jump to the first command in the next `#` section; wraps back to the first section.
- Type: filter commands case-insensitively. Matching section headings remain visible.
- `Backspace`: edit the search query.
- `Ctrl+U`: clear the search query.
- `Enter`: run the selected command in the original Herdr pane.
- `?`: open/close the built-in help view.
- `Esc` or `Ctrl+C`: close Command List.

## Shortcut

The local installer adds this block when `Ctrl+Y` is available:

```toml
# Command List shortcut.
# Change "ctrl+y" to another Herdr key (for example "prefix+y") to customize it.
[[keys.command]]
key = "ctrl+y"
type = "plugin_action"
command = "herdr.command-list.toggle"
description = "Command List"
```

Herdr reads the configuration from `~/.config/herdr/config.toml` on Linux and macOS unless `HERDR_CONFIG_PATH` overrides it.

Reload after manual config changes:

```sh
herdr server reload-config
```

## Security

- All production Go code lives in `main.go`.
- There are no third-party Go modules, telemetry, network clients, or daemons.
- `commands.md` is re-read each time the popup opens.
- Commands and displayed section headings reject invalid UTF-8, control characters, ANSI/escape sequences, tabs, Unicode bidi/format characters, zero-width characters, and Unicode line/paragraph separators.
- The same command validation runs again immediately before `herdr pane run`.
- `commands.md` must be a regular file; symbolic links and special files are rejected.
- The plugin configuration directory must be a real directory and must not be a symbolic link.
- Directory permissions are hardened to `0700`; `commands.md` and the installed `HELP.md` use `0600`.
- No `sh -c` is used by the plugin when submitting a command. The selected text is passed as a single argument to `herdr pane run`.
- No command is selected when the popup first opens.
- `commands.md` intentionally contains executable input. A dangerous command stored there remains dangerous and runs after you select it and press Enter.



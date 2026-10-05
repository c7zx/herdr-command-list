# Changelog

## 1.0.2 — 2026-10-05

Changes from **1.0.1**:

### Added

- Multiple named command lists stored as `.md` files in a persistent `lists/` directory.
- Tab bar with `Left` / `Right` switching and wraparound.
- Three starter lists: `Main`, `Systems`, and `More`.
- Automatic reload of lists whenever the popup opens.
- Safe uninstall script that removes the plugin while preserving personal command lists.
- Bounded list loading and bracketed-paste protection.

### Changed

- Plugin name and ID changed from **Command List** / `herdr.command-list` to **Command Lists** / `herdr.command-lists`.
- `Up` / `Down` navigation now wraps and scrolls naturally instead of keeping the selection pinned to an edge.
- Running a selected command first clears unfinished shell input with `Ctrl+E` and `Ctrl+U`.
- Existing `.md` lists are preserved during reinstall; starter lists are created only when the list directory is missing or empty.
- Help and examples were updated for the multi-list interface.

Command Lists v1.0.2
===================

Default shortcut: Ctrl+Y (when that key is available).

Controls
--------
Up / Down       Select command; wrap at both ends
Left / Right    Previous / next list; wrap at both ends
Ctrl+Space      Next section; clear filter and wrap
Type            Filter this list
Backspace       Edit filter
Ctrl+U          Clear filter
Enter           Replace input and run selection
?               Open / close this help
Esc / Ctrl+C    Close popup

Nothing is selected initially. Down starts at the first
command; Up starts at the last. The list scrolls only when
the selection reaches an edge.

Lists
-----
Each .md file in the lists directory becomes one tab.
The filename without .md is the tab name. Long names are
shortened in the tab bar. Lists reload when the popup opens.

# lines are headings. Blank lines add spacing. Every other
non-empty line is one shell command. Leading spaces add
visual indentation. Use one physical line per command.

Edit lists from a shell:

cfg=$(herdr plugin config-dir herdr.command-lists)
nano "$cfg/lists/Main.md"

Running commands
----------------
Enter sends Ctrl+E and Ctrl+U before the selected command.
Use a normal single-line shell prompt with standard Emacs
editing keys. Editors, running apps, multiline prompts, and
Vi/custom bindings may interpret those keys differently.

Bracketed paste is ignored; type the filter deliberately.

# demo.tape — records a go-test-tui session as a GIF.
#
# Run from a Go project directory that has tests:
#   vhs demo.tape
#
# Output: demo.gif (update README.md to reference it)

Output demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 160
Set Height 44
Set Theme "Catppuccin Mocha"
Set PlaybackSpeed 0.75

# ── help ─────────────────────────────────────────────────────────────────────

Type "go-test-tui help"
Sleep 300ms
Enter
Sleep 3s

# ── run the TUI ──────────────────────────────────────────────────────────────

Ctrl+L
Sleep 300ms

Type "go-test-tui"
Sleep 300ms
Enter

# Wait for tests to start populating
Sleep 4s

# Navigate down through the list
Down
Sleep 300ms
Down
Sleep 300ms
Down
Sleep 600ms

# Page down
PageDown
Sleep 600ms

# Filter
Type "/"
Sleep 300ms
Type "Test"
Sleep 800ms
Escape
Sleep 500ms

# Jump to bottom (pinned / newest)
Type "G"
Sleep 500ms

# Switch focus to log panel
Tab
Sleep 500ms

# Scroll log
Down
Sleep 200ms
Down
Sleep 200ms
Down
Sleep 200ms

# Switch back
Tab
Sleep 300ms

# Wait for tests to finish
Sleep 8s

# Quit
Type "q"
Sleep 500ms

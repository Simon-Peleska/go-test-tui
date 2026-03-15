# go-test-tui

A terminal UI for running Go tests. Shows a live test list on the left and streaming log output on the right. Designed to be dropped into any Go project.

## Demo

![demo](demo.gif)

To record a new demo, run from a Go project directory with tests:

```bash
vhs demo.tape
```

## Running

### Without Nix

**Dependencies:** Go 1.21+

```bash
go build -o go-test-tui .
./go-test-tui
```

### With Nix

```bash
# Run directly
nix run

# Build first
nix build
./result/bin/go-test-tui

# Dev shell
nix develop
```

## Usage

```bash
# Run all tests in the current directory
go-test-tui

# Keep logs even when tests pass
go-test-tui -keep-logs

# Custom log directory
go-test-tui -output-dir /tmp/test-logs

# Clean old logs before running
go-test-tui -clean

# Pass flags directly to go test (after --)
go-test-tui -- -run TestFoo
go-test-tui -- -run TestFoo -count 2 -parallel 4

# Show help
go-test-tui help
```

## Subcommands

### `list` — inspect the last run without re-running

```bash
# List all tests from the last run
go-test-tui list

# Only failed tests
go-test-tui list -status failed

# Only skipped tests
go-test-tui list -status skip

# Show the full log for a specific test
go-test-tui list -status failed TestWerewolfCanVote
```

Output includes the test name, source file and line range, and log file path.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-output-dir` | `./test_logs` | Directory for log files |
| `-keep-logs` | `false` | Keep logs even if all tests pass |
| `-clean` | `false` | Remove old log directories before running |

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `pgup` / `ctrl+u` | Page up |
| `pgdn` / `ctrl+d` | Page down |
| `g` / `G` | Jump to top / bottom |
| `/` | Filter test list |
| `esc` | Deselect (show combined log) |
| `tab` | Switch focus between panels |
| `q` / `ctrl+c` | Quit |

## Log files

Each run creates a timestamped directory under `-output-dir`:

```
test_logs/
  latest -> run_20260315_143021   (symlink)
  run_20260315_143021/
    test_output.json              (raw go test -json stream)
    TestFoo/
      test_output.log
    TestBar/
      test_output.log
```

Tests can write their own artifact files under `$TEST_OUTPUT_DIR` (set automatically to the run directory).

## Integrating with a project

The tool is generic — drop it in as-is. If your project needs additional env vars set before `go test` runs (e.g. feature flags, log levels), wrap `go-test-tui` in a small shell script:

```bash
#!/usr/bin/env bash
export MY_PROJECT_DEBUG=1
export MY_PROJECT_LOG_DB=1
exec go-test-tui "$@"
```

## Tech Stack

- **Language**: Go
- **TUI**: Bubble Tea v2, Bubbles v2, Lip Gloss v2 (Charm)
- **Packaging**: Nix flake

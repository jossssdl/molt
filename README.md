# molt

**English** · [Español](README.es.md)

Fast terminal UI for reclaiming disk space from developer artifacts and local AI caches, with Git-aware safety.

molt scans your project folders for things you can regenerate (`node_modules`, Python virtualenvs, Rust/Maven `target`, `.next`) and, optionally, machine-wide AI caches (Hugging Face, pip, orphaned Ollama blobs). Each item gets a safety badge based on the state of the Git repository that owns it, so you never delete a build folder next to work you haven't committed.

## What it does and why

**Main function:** molt finds the folders on your disk that are big, disposable and regenerable, tells you whether deleting each one is safe, and lets you remove them in a few keystrokes.

**The problem.** Every project you touch leaves heavy folders behind: `node_modules` (hundreds of MB each), `.next` and `target` build output, Python virtualenvs, plus global caches from AI tooling (Hugging Face models, pip wheels, Ollama blobs left by removed models). Across dozens of old projects this quietly eats tens or hundreds of GB. None of it is source code: `npm install`, `cargo build` or re-downloading a model brings it back.

**Why existing approaches fall short.** Deleting by hand means hunting folder by folder, and running `rm -rf` on a project you are still working on, or in a repo with uncommitted changes, is easy to regret. Generic disk analyzers show sizes but know nothing about Git or about what is safe to remove.

**What molt does about it:**
1. **Finds** only known, regenerable targets, with extra checks to avoid false positives (for example `target` only counts next to a `Cargo.toml` or `pom.xml`).
2. **Measures** them in parallel, so scanning a whole drive stays fast and the interface never freezes.
3. **Checks Git** for the repository that owns each folder (uncommitted changes, untracked files, unpushed commits, no upstream) and labels it `SAFE`, `DIRTY GIT` or `UNKNOWN`. Orphaned Ollama blobs are only reported when every manifest could be read, so a model you use is never flagged.
4. **Shows how stale it is** (time since the last commit or file change), so old, untouched projects float to the top.
5. **Deletes carefully**: nothing is removed in the default dry-run mode, you confirm explicitly, risky items need a second explicit choice, and every path is re-validated right before deletion. Only one package (`internal/cleaner`) is allowed to delete anything.

In short: *free the most space with the least risk of losing work.*

## Features

- **Concurrent scanner**: one directory walker per root feeding a worker pool that measures sizes; results stream into the UI while the scan runs.
- **Git guard**: every item inside a repository is checked once per repo for uncommitted changes, untracked files, unpushed commits, a missing upstream, and whether Git tracks the target itself.
  - `SAFE`: nothing known to Git would be lost.
  - `DIRTY GIT`: the repo has unsaved work (shown in the detail line).
  - `UNKNOWN`: the state could not be determined. Treated as unsafe.
- **Interactive table**: sort by size or by inactivity (latest commit or file change), filter (All / Safe / Risky / Dev / AI), multi-select with keyboard or mouse, and a bar showing selected / reclaimable / total space.
- **Safe deletion**: dry-run by default. Any item can be selected, but when the selection contains `DIRTY GIT` or `UNKNOWN` items molt asks whether to delete only the `SAFE` ones or force all of them. The cleaner re-validates every path right before deleting it.

### Detected targets

| Target | Rule |
| --- | --- |
| `node_modules` | any directory with that name |
| `.venv` / `venv` | contains `pyvenv.cfg` |
| `target` | next to `Cargo.toml` or `pom.xml` |
| `.next` | next to `package.json` |
| Hugging Face cache | `HF_HOME`, `$XDG_CACHE_HOME/huggingface`, `~/.cache/huggingface` (`-ai-caches`) |
| pip cache | `PIP_CACHE_DIR`, `~/.cache/pip`, `%LOCALAPPDATA%\pip\Cache` (`-ai-caches`) |
| Ollama orphan blobs | `sha256-*` blobs not referenced by any manifest in `OLLAMA_MODELS` or `~/.ollama/models` (`-ai-caches`) |

Orphan detection refuses to guess: if the Ollama manifests are missing or unreadable, nothing is reported.

## Install

Requires Go 1.24+ and `git` on your `PATH`.

```sh
go install github.com/jossssdl/molt/cmd/molt@latest
```

Or build from source:

```sh
git clone https://github.com/jossssdl/molt
cd molt
go build -o molt ./cmd/molt
```

## Usage

```sh
molt                           # scan the current directory
molt -root ~/projects          # scan a folder (repeatable)
molt -root ~/projects -ai-caches
molt -list -root ~/projects    # print a table instead of opening the TUI
molt -json -root ~/projects    # machine-readable output
molt -dry-run=false -root ~/projects   # really delete (still asks for confirmation)
```

| Flag | Description |
| --- | --- |
| `-root <dir>` | Directory to scan; repeat for several. Defaults to the current directory. |
| `-ai-caches` | Also scan AI caches. |
| `-workers <n>` | Measuring workers (default: number of CPUs). |
| `-list` | Print a table and exit. |
| `-json` | Print JSON and exit. |
| `-dry-run` | `true` by default: deletions are only simulated. Use `-dry-run=false` to delete for real. |

### Keys

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k`, `PgUp` `PgDn`, `g` `G` | Move |
| `space` | Select / unselect the current item and move down |
| `a` / `A` / `n` | Select all `SAFE` in view / select everything in view / clear selection |
| `tab` / `shift+tab`, `1`–`5` | Change filter |
| `s` / `i` | Sort by size / by inactivity |
| `d` or `enter` | Delete selected items (asks for confirmation) |
| `q` / `esc` | Quit |
| mouse | Click a row to select it, wheel to scroll |

## Development

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...   # requires cgo (a C compiler)
```

Layout:

```
cmd/molt            entrypoint and flags
internal/targets    detection rules
internal/scanner    concurrent discovery and size measurement
internal/gitguard   Git safety checks
internal/cleaner    the only package that deletes anything
internal/tui        Bubbletea interface
```

## License

See [LICENSE](LICENSE).

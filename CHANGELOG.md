# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [0.1.0] - Unreleased

First public release.

### Added

**Scanning**
- Concurrent scanner: one directory walker per root feeding a worker pool that measures sizes; results stream to the UI while the scan is still running. Cancellable with `Ctrl+C`.
- Development targets with false-positive guards: `node_modules`, `.venv`/`venv` (requires `pyvenv.cfg`), `target` (next to `Cargo.toml` or `pom.xml`) and `.next` (next to `package.json`). Nested matches are reported once, inside their outermost parent.
- More project targets (git-aware, validated by a sibling file): Swift Package Manager `.build` (`Package.swift`), Gradle `.gradle`/`build` (`build.gradle[.kts]`), Terraform `.terraform` (`*.tf` or `.terraform.lock.hcl`) and CocoaPods `Pods` (`Podfile`).
- Local AI and machine-wide cache targets, enabled with `-ai-caches`:
  - Hugging Face: one item per `models--*` and `datasets--*` folder (`HF_HUB_CACHE`, `HF_HOME`, `XDG_CACHE_HOME`, `~/.cache/huggingface/hub`), listed as `HF Model: org/name`. Tokens and other files are never touched.
  - Ollama: orphaned blobs that no manifest references. Nothing is reported if the manifests cannot be read, and recent `-partial` downloads are ignored.
  - llama.cpp: each `*.gguf` model plus stale partial downloads (`LLAMA_CACHE`, `~/.cache/llama.cpp`, macOS and Windows locations).
  - LM Studio: each `<publisher>/<model>` folder (`~/.lmstudio/models`, `~/.cache/lm-studio/models`).
  - vLLM cache (`VLLM_CACHE_ROOT`, `~/.cache/vllm`; Linux/macOS) and Xcode DerivedData, one item per project (macOS only).
  - PyTorch hub (`TORCH_HOME`), `uv` cache (`UV_CACHE_DIR`) and `pip` cache.
- Filters: `-min-size` (e.g. `100MB`), `-min-age` (e.g. `30d`, `2w`, `12h`; based on the last file change or commit) and `-exclude` (folder names, globs or absolute paths; repeatable or comma-separated).
- Unreadable folders are reported instead of skipped silently: `⚠ N paths skipped (permission/error)` in the TUI and a summary on stderr in `-list` / `-json`.

**Git protection**
- Each item inside a repository gets a verdict computed once per repo: `SAFE`, `DIRTY GIT` (uncommitted changes, untracked files, unpushed commits, branch without upstream, or files tracked inside the target) or `UNKNOWN` (any git error, treated as unsafe).
- The detail panel spells out the exact reasons, the repository root and the last commit date.

**Interface**
- Interactive TUI (Bubble Tea): sortable by size or idle time, filters (All / Safe / Risky / Dev / AI), keyboard and mouse multi-select, reclaimable-space bar and detail panel.
- Report modes: `-list` (table) and `-json`.
- `-version`, injectable with `-ldflags "-X main.version=..."`.

**Safe deletion**
- `-dry-run` is `true` by default; real deletion requires `-dry-run=false` and an explicit confirmation.
- Selections that include `DIRTY GIT` or `UNKNOWN` items require a second decision: delete only the `SAFE` ones, or force all.
- Every path is re-validated against its rule right before deletion; relative paths, filesystem roots and the home directory (or its parents) are refused.
- Deletion copes with read-only files and directories, and never follows symlinks or junctions: a link inside `node_modules` (for example into a pnpm store) is removed, its destination is left intact.

**Project**
- `install.sh` one-liner for Linux and macOS: detects OS and architecture, downloads the latest release, verifies its SHA-256 checksum and installs into `/usr/local/bin` or `~/.local/bin`.
- CI on Linux, macOS and Windows (`go vet`, `go test -race`) and automated releases with GoReleaser (linux/darwin/windows on amd64 and arm64, with checksums).
- `scripts/sandbox`: generates a disposable scenario (SAFE, DIRTY and UNPUSHED repos plus fake Hugging Face and llama.cpp caches) for trying the TUI and real deletion safely.
- English and Spanish READMEs.

### Performance
- Location targets are resolved once per cleaning batch instead of once per item, removing a quadratic cost with large Ollama stores.

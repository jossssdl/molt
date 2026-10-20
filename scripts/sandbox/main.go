// Command sandbox builds a disposable test scenario for trying molt by hand,
// including real deletion, without touching anything outside ./.sandbox.
//
//	go run ./scripts/sandbox
//
// Everything lives under the output directory, including a fake HOME with the
// Hugging Face and llama.cpp caches. The printed commands point molt at that
// fake HOME so none of your real caches are ever listed or deleted.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const marker = ".molt-sandbox"

func main() {
	out := flag.String("dir", ".sandbox", "output directory (recreated on every run)")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "sandbox:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := reset(root); err != nil {
		return err
	}
	home := filepath.Join(root, "home")
	remotes := filepath.Join(root, "_remotes")
	projects := filepath.Join(root, "projects")

	steps := []func() error{
		func() error { return safeProject(filepath.Join(projects, "safe-web"), remotes) },
		func() error { return dirtyProject(filepath.Join(projects, "dirty-api"), remotes) },
		func() error { return unpushedProject(filepath.Join(projects, "unpushed-cli"), remotes) },
		func() error { return aiCaches(home) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			return err
		}
	}
	return printInstructions(root, home)
}

// reset recreates dir, refusing to delete anything that is not a previous sandbox.
func reset(root string) error {
	if _, err := os.Stat(root); err == nil {
		if _, err := os.Stat(filepath.Join(root, marker)); err != nil {
			return fmt.Errorf("%s exists and is not a molt sandbox (no %s marker); refusing to delete it", root, marker)
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, marker), []byte("created by scripts/sandbox\n"), 0o644)
}

// git runs git without reading or writing any user configuration.
func git(dir string, args ...string) error {
	full := append([]string{
		"-c", "user.name=sandbox", "-c", "user.email=sandbox@example.invalid",
		"-c", "commit.gpgsign=false", "-C", dir,
	}, args...)
	cmd := exec.Command("git", full...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, b)
	}
	return nil
}

func write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// filler writes n files of size bytes of deterministic noise below dir.
func filler(dir string, n, size int) error {
	rng := rand.New(rand.NewSource(42))
	buf := make([]byte, size)
	for i := 0; i < n; i++ {
		rng.Read(buf)
		p := filepath.Join(dir, fmt.Sprintf("pkg%02d", i/5), fmt.Sprintf("file%02d.bin", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, buf, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// newRepo creates a repo with an initial pushed commit to a bare remote.
func newRepo(dir, remotes string, files map[string]string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, content := range files {
		if err := write(filepath.Join(dir, name), content); err != nil {
			return err
		}
	}
	remote := filepath.Join(remotes, filepath.Base(dir)+".git")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		return err
	}
	for _, step := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-q", "-m", "initial commit"},
	} {
		if err := git(dir, step...); err != nil {
			return err
		}
	}
	if err := git(remote, "init", "-q", "--bare", "-b", "main"); err != nil {
		return err
	}
	if err := git(dir, "remote", "add", "origin", remote); err != nil {
		return err
	}
	return git(dir, "push", "-q", "-u", "origin", "main")
}

// SAFE: clean, pushed repo; node_modules is git-ignored.
func safeProject(dir, remotes string) error {
	if err := newRepo(dir, remotes, map[string]string{
		"package.json": `{"name":"safe-web","version":"1.0.0"}` + "\n",
		"src/index.js": "console.log('hello');\n",
		".gitignore":   "node_modules/\n",
	}); err != nil {
		return err
	}
	return filler(filepath.Join(dir, "node_modules"), 40, 100*1024) // ~4 MB
}

// DIRTY: pushed repo with a modified tracked file and an untracked file.
func dirtyProject(dir, remotes string) error {
	if err := newRepo(dir, remotes, map[string]string{
		"main.py":    "print('v1')\n",
		".gitignore": ".venv/\n",
	}); err != nil {
		return err
	}
	venv := filepath.Join(dir, ".venv")
	if err := write(filepath.Join(venv, "pyvenv.cfg"), "home = /usr/bin\nversion = 3.12.0\n"); err != nil {
		return err
	}
	if err := filler(filepath.Join(venv, "lib"), 30, 100*1024); err != nil { // ~3 MB
		return err
	}
	if err := write(filepath.Join(dir, "main.py"), "print('v2 - not committed')\n"); err != nil {
		return err
	}
	return write(filepath.Join(dir, "notes.txt"), "work in progress\n")
}

// UNPUSHED: clean tree, but one local commit missing from the remote.
func unpushedProject(dir, remotes string) error {
	if err := newRepo(dir, remotes, map[string]string{
		"Cargo.toml":  "[package]\nname = \"unpushed-cli\"\nversion = \"0.1.0\"\nedition = \"2021\"\n",
		"src/main.rs": "fn main() {}\n",
		".gitignore":  "target/\n",
	}); err != nil {
		return err
	}
	if err := write(filepath.Join(dir, "src", "lib.rs"), "pub fn answer() -> u32 { 42 }\n"); err != nil {
		return err
	}
	if err := git(dir, "add", "-A"); err != nil {
		return err
	}
	if err := git(dir, "commit", "-q", "-m", "local work, not pushed"); err != nil {
		return err
	}
	return filler(filepath.Join(dir, "target", "debug"), 40, 100*1024) // ~4 MB
}

// aiCaches lays out fake Hugging Face and llama.cpp caches under a fake HOME.
func aiCaches(home string) error {
	hf := filepath.Join(home, ".cache", "huggingface", "hub")
	model := filepath.Join(hf, "models--meta-llama--Llama-3.2-1B")
	if err := filler(filepath.Join(model, "blobs"), 8, 512*1024); err != nil { // ~4 MB
		return err
	}
	if err := write(filepath.Join(model, "refs", "main"), "0123456789abcdef\n"); err != nil {
		return err
	}
	if err := write(filepath.Join(hf, "datasets--demo--tiny", "refs", "main"), "fedcba9876543210\n"); err != nil {
		return err
	}
	if err := write(filepath.Join(home, ".cache", "huggingface", "token"), "hf_FAKE_TOKEN_must_survive\n"); err != nil {
		return err
	}
	buf := make([]byte, 3*1024*1024) // one ~3 MB "model"
	rand.New(rand.NewSource(7)).Read(buf)
	llama := filepath.Join(home, ".cache", "llama.cpp", "model.gguf")
	if err := os.MkdirAll(filepath.Dir(llama), 0o755); err != nil {
		return err
	}
	return os.WriteFile(llama, buf, 0o644)
}

// Launchers set a fake HOME (and unset every cache override) only for the
// duration of the run, so molt can never see or delete real AI caches.
const launcherPS1 = `# Runs molt against the sandbox with a fake HOME. Extra arguments are passed through.
$sandbox = $PSScriptRoot
$fakeHome = Join-Path $sandbox 'home'
$molt = if ($env:MOLT_BIN) { $env:MOLT_BIN } else { '.\molt.exe' }
$names = 'USERPROFILE','HOME','LOCALAPPDATA','XDG_CACHE_HOME','HF_HOME','HF_HUB_CACHE','LLAMA_CACHE','UV_CACHE_DIR','TORCH_HOME','PIP_CACHE_DIR','OLLAMA_MODELS'
$saved = @{}; foreach ($n in $names) { $saved[$n] = [Environment]::GetEnvironmentVariable($n) }
try {
    foreach ($n in $names[3..($names.Length-1)]) { [Environment]::SetEnvironmentVariable($n, $null) }
    $env:USERPROFILE = $fakeHome; $env:HOME = $fakeHome
    $env:LOCALAPPDATA = Join-Path $fakeHome 'AppData\Local'
    & $molt -root $sandbox -ai-caches @args
} finally {
    foreach ($n in $names) { [Environment]::SetEnvironmentVariable($n, $saved[$n]) }
}
`

const launcherSH = `#!/bin/sh
# Runs molt against the sandbox with a fake HOME. Extra arguments are passed through.
sandbox="$(cd "$(dirname "$0")" && pwd)"
exec env -u XDG_CACHE_HOME -u HF_HOME -u HF_HUB_CACHE -u LLAMA_CACHE -u UV_CACHE_DIR \
    -u TORCH_HOME -u PIP_CACHE_DIR -u OLLAMA_MODELS HOME="$sandbox/home" \
    "${MOLT_BIN:-./molt}" -root "$sandbox" -ai-caches "$@"
`

func printInstructions(root, home string) error {
	if err := write(filepath.Join(root, "run.ps1"), launcherPS1); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte(launcherSH), 0o755); err != nil {
		return err
	}

	fmt.Printf("Sandbox ready in %s\n\n", root)
	fmt.Println("What was created (expected verdict in molt):")
	fmt.Println("  projects/safe-web       node_modules  ~4 MB   SAFE       (clean, pushed, ignored by git)")
	fmt.Println("  projects/dirty-api      .venv         ~3 MB   DIRTY GIT  (1 modified + 1 untracked file)")
	fmt.Println("  projects/unpushed-cli   target        ~4 MB   DIRTY GIT  (1 commit not pushed)")
	fmt.Println("  home/.cache/huggingface HF Model: meta-llama/Llama-3.2-1B  ~4 MB  SAFE (+ a dataset; the token file is never listed)")
	fmt.Println("  home/.cache/llama.cpp   model.gguf    ~3 MB   SAFE")
	fmt.Println()
	fmt.Println("The launcher scripts run molt with a FAKE home (home/), so your real AI caches are never")
	fmt.Println("listed or deleted. They also pass -root .sandbox and -ai-caches for you.")
	fmt.Println()
	fmt.Println("Build molt once, from the repository root:")
	fmt.Println("  go build -o molt ./cmd/molt        (Windows: go build -o molt.exe ./cmd/molt)")
	fmt.Println()
	if runtime.GOOS == "windows" {
		fmt.Println("Then, in PowerShell from the repository root:")
		fmt.Println(`  .\.sandbox\run.ps1 -list              # table, nothing deleted`)
		fmt.Println(`  .\.sandbox\run.ps1                    # TUI in dry-run (deletion only simulated)`)
		fmt.Println(`  .\.sandbox\run.ps1 -dry-run=false     # TUI with REAL deletion`)
		fmt.Println("(Linux/macOS launcher: .sandbox/run.sh with the same arguments.)")
	} else {
		fmt.Println("Then, from the repository root:")
		fmt.Println("  .sandbox/run.sh -list              # table, nothing deleted")
		fmt.Println("  .sandbox/run.sh                    # TUI in dry-run (deletion only simulated)")
		fmt.Println("  .sandbox/run.sh -dry-run=false     # TUI with REAL deletion")
		fmt.Println("(Windows launcher: .sandbox\run.ps1 with the same arguments.)")
	}
	fmt.Println()
	fmt.Println("Suggested manual checks:")
	fmt.Println("  - In the TUI press 'a' (select all SAFE), 'd', then 'y': only SAFE items go.")
	fmt.Println("  - Press 'A' then 'd': the dialog must offer [y] only SAFE / [f] ALL / [n] cancel.")
	fmt.Println("  - After real deletion, check that projects/*/src, main.py, notes.txt and home/.cache/huggingface/token are intact.")
	fmt.Println("  - Re-run `go run ./scripts/sandbox` to rebuild the scenario from scratch.")
	_ = home
	return nil
}

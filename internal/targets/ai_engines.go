package targets

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This file holds the local-inference engines beyond Hugging Face and Ollama:
// llama.cpp, LM Studio, the PyTorch hub and the uv package cache. Path logic is
// split from environment access (env/home/goos are parameters) so every
// platform can be tested on any machine.

// envFunc looks up an environment variable.
type envFunc func(string) string

// userHome returns the home directory, or "" when unknown.
func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// llamaCacheDirs lists candidate llama.cpp cache directories: LLAMA_CACHE,
// ~/.cache/llama.cpp (XDG_CACHE_HOME aware), ~/Library/Caches/llama.cpp on
// macOS and %LOCALAPPDATA%\llama.cpp on Windows.
func llamaCacheDirs(env envFunc, home, goos string) []string {
	var c []string
	if v := env("LLAMA_CACHE"); v != "" {
		c = append(c, v)
	}
	if v := env("XDG_CACHE_HOME"); v != "" {
		c = append(c, filepath.Join(v, "llama.cpp"))
	}
	if home != "" {
		c = append(c, filepath.Join(home, ".cache", "llama.cpp"))
		if goos == "darwin" {
			c = append(c, filepath.Join(home, "Library", "Caches", "llama.cpp"))
		}
	}
	if goos == "windows" {
		if v := env("LOCALAPPDATA"); v != "" {
			c = append(c, filepath.Join(v, "llama.cpp"))
		}
	}
	return c
}

// llamaFiles returns the downloaded models in the given cache directories:
// every *.gguf file, plus interrupted downloads (*.partial,
// *.downloadInProgress) that have been idle for at least partialGrace.
func llamaFiles(dirs []string, now time.Time) []string {
	existing, _ := existingDirs(dirs)
	seen := make(map[string]bool)
	var out []string
	for _, d := range existing {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if e != nil && e.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if e.IsDir() || !e.Type().IsRegular() || seen[p] {
				return nil
			}
			lower := strings.ToLower(e.Name())
			switch {
			case strings.HasSuffix(lower, ".gguf"):
			case strings.HasSuffix(lower, ".partial") || strings.HasSuffix(lower, ".downloadinprogress"):
				info, ierr := e.Info()
				if ierr != nil || now.Sub(info.ModTime()) < partialGrace {
					return nil // possibly an active download
				}
			default:
				return nil
			}
			seen[p] = true
			out = append(out, p)
			return nil
		})
	}
	return out
}

// LlamaCppCache reports each GGUF model downloaded into the llama.cpp cache
// (llama-cli/llama-server -hf), one item per file, and stale partial downloads.
func LlamaCppCache() Target {
	return locationTarget{
		name: "llama.cpp model",
		resolve: func() ([]string, error) {
			return llamaFiles(llamaCacheDirs(os.Getenv, userHome(), goos), time.Now()), nil
		},
		label: func(p string) string { return "llama.cpp: " + filepath.Base(p) },
	}
}

// lmStudioRoots lists candidate LM Studio model directories.
func lmStudioRoots(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".lmstudio", "models"),
		filepath.Join(home, ".cache", "lm-studio", "models"),
	}
}

// lmStudioModels returns every <root>/<publisher>/<model> directory, skipping
// hidden entries (names starting with ".") at both levels.
func lmStudioModels(roots []string) []string {
	existing, _ := existingDirs(roots)
	seen := make(map[string]bool)
	var out []string
	for _, root := range existing {
		pubs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, pub := range pubs {
			if !pub.IsDir() || strings.HasPrefix(pub.Name(), ".") {
				continue
			}
			models, err := os.ReadDir(filepath.Join(root, pub.Name()))
			if err != nil {
				continue
			}
			for _, m := range models {
				if !m.IsDir() || strings.HasPrefix(m.Name(), ".") {
					continue
				}
				p := filepath.Join(root, pub.Name(), m.Name())
				if !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// LMStudioModels reports each downloaded LM Studio model (<publisher>/<model>)
// as its own item.
func LMStudioModels() Target {
	return locationTarget{
		name:    "lm studio model",
		resolve: func() ([]string, error) { return lmStudioModels(lmStudioRoots(userHome())), nil },
		label: func(p string) string {
			return "LM Studio: " + filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
		},
	}
}

// torchHubDirs lists candidate PyTorch hub directories: $TORCH_HOME/hub,
// $XDG_CACHE_HOME/torch/hub and ~/.cache/torch/hub.
func torchHubDirs(env envFunc, home string) []string {
	var c []string
	if v := env("TORCH_HOME"); v != "" {
		c = append(c, filepath.Join(v, "hub"))
	}
	if v := env("XDG_CACHE_HOME"); v != "" {
		c = append(c, filepath.Join(v, "torch", "hub"))
	}
	if home != "" {
		c = append(c, filepath.Join(home, ".cache", "torch", "hub"))
	}
	return c
}

// PyTorchHub is the torch.hub download cache (checkpoints and cloned repos).
func PyTorchHub() Target {
	return locationTarget{name: "pytorch hub", resolve: func() ([]string, error) {
		return existingDirs(torchHubDirs(os.Getenv, userHome()))
	}}
}

// uvCacheDirs lists candidate uv cache directories: UV_CACHE_DIR,
// $XDG_CACHE_HOME/uv, ~/.cache/uv, ~/Library/Caches/uv on macOS and
// %LOCALAPPDATA%\uv\cache on Windows.
func uvCacheDirs(env envFunc, home, goos string) []string {
	var c []string
	if v := env("UV_CACHE_DIR"); v != "" {
		c = append(c, v)
	}
	if v := env("XDG_CACHE_HOME"); v != "" {
		c = append(c, filepath.Join(v, "uv"))
	}
	if home != "" {
		c = append(c, filepath.Join(home, ".cache", "uv"))
		if goos == "darwin" {
			c = append(c, filepath.Join(home, "Library", "Caches", "uv"))
		}
	}
	if goos == "windows" {
		if v := env("LOCALAPPDATA"); v != "" {
			c = append(c, filepath.Join(v, "uv", "cache"))
		}
	}
	return c
}

// UvCache is the cache of the uv Python package manager.
func UvCache() Target {
	return locationTarget{name: "uv cache", resolve: func() ([]string, error) {
		return existingDirs(uvCacheDirs(os.Getenv, userHome(), goos))
	}}
}

// vllmCacheDirs lists candidate vLLM cache directories: VLLM_CACHE_ROOT,
// $XDG_CACHE_HOME/vllm and ~/.cache/vllm. vLLM does not run natively on
// Windows, so nothing is reported there.
func vllmCacheDirs(env envFunc, home, goos string) []string {
	if goos == "windows" {
		return nil
	}
	var c []string
	if v := env("VLLM_CACHE_ROOT"); v != "" {
		c = append(c, v)
	}
	if v := env("XDG_CACHE_HOME"); v != "" {
		c = append(c, filepath.Join(v, "vllm"))
	}
	if home != "" {
		c = append(c, filepath.Join(home, ".cache", "vllm"))
	}
	return c
}

// VllmCache is the vLLM cache (compiled artifacts and downloaded assets).
func VllmCache() Target {
	return locationTarget{name: "vllm cache", resolve: func() ([]string, error) {
		return existingDirs(vllmCacheDirs(os.Getenv, userHome(), goos))
	}}
}

// xcodeDerivedDataDir is ~/Library/Developer/Xcode/DerivedData on macOS and ""
// everywhere else.
func xcodeDerivedDataDir(home, goos string) string {
	if goos != "darwin" || home == "" {
		return ""
	}
	return filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
}

// xcodeProjects returns the per-project directories inside a DerivedData
// directory, skipping hidden entries and plain files.
func xcodeProjects(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// xcodeHash matches the "-<28 lowercase letters>" suffix Xcode appends to the
// project name.
var xcodeHash = regexp.MustCompile(`-[a-z]{28}$`)

// XcodeDerivedData reports each project folder of Xcode's DerivedData (macOS
// only; build products and indexes that Xcode regenerates).
func XcodeDerivedData() Target {
	return locationTarget{
		name:     "xcode derived",
		category: CategoryDev,
		resolve: func() ([]string, error) {
			return xcodeProjects(xcodeDerivedDataDir(userHome(), goos)), nil
		},
		label: func(p string) string {
			return "Xcode DerivedData: " + xcodeHash.ReplaceAllString(filepath.Base(p), "")
		},
	}
}

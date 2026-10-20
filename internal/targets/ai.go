package targets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// partialGrace is how old an Ollama "-partial" download must be before it is
// reported, so an in-progress pull is never offered for deletion.
const partialGrace = 24 * time.Hour

// locationTarget is a LocationTarget backed by a resolver function.
type locationTarget struct {
	name    string
	resolve func() ([]string, error)
}

func (t locationTarget) Name() string               { return t.name }
func (locationTarget) Category() Category           { return CategoryAI }
func (locationTarget) GitAware() bool               { return false }
func (t locationTarget) Resolve() ([]string, error) { return t.resolve() }

// HuggingFace is the Hugging Face cache (HF_HOME, XDG_CACHE_HOME/huggingface
// or ~/.cache/huggingface).
func HuggingFace() Target {
	return locationTarget{name: "huggingface", resolve: func() ([]string, error) {
		var c []string
		if v := os.Getenv("HF_HOME"); v != "" {
			c = append(c, v)
		}
		if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
			c = append(c, filepath.Join(v, "huggingface"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			c = append(c, filepath.Join(home, ".cache", "huggingface"))
		}
		return existingDirs(c)
	}}
}

// Pip is the pip download/wheel cache.
func Pip() Target {
	return locationTarget{name: "pip cache", resolve: func() ([]string, error) {
		var c []string
		if v := os.Getenv("PIP_CACHE_DIR"); v != "" {
			c = append(c, v)
		}
		if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
			c = append(c, filepath.Join(v, "pip"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			c = append(c, filepath.Join(home, ".cache", "pip"))
			if runtime.GOOS == "darwin" {
				c = append(c, filepath.Join(home, "Library", "Caches", "pip"))
			}
		}
		if runtime.GOOS == "windows" {
			if v := os.Getenv("LOCALAPPDATA"); v != "" {
				c = append(c, filepath.Join(v, "pip", "Cache"))
			}
		}
		return existingDirs(c)
	}}
}

// OllamaOrphans reports blobs in the Ollama store that no manifest references
// (left behind by removed or half-pulled models), one result per blob.
func OllamaOrphans() Target {
	return locationTarget{name: "ollama orphan blob", resolve: func() ([]string, error) {
		dir := os.Getenv("OLLAMA_MODELS")
		if dir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			dir = filepath.Join(home, ".ollama", "models")
		}
		return ollamaOrphans(dir, time.Now())
	}}
}

// existingDirs returns the cleaned, de-duplicated candidates that are
// directories. A machine with none of them is not an error.
func existingDirs(candidates []string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string
	for _, c := range candidates {
		c = filepath.Clean(c)
		if seen[c] {
			continue
		}
		seen[c] = true
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			out = append(out, c)
		}
	}
	return out, nil
}

// ollamaOrphans lists blobs under modelsDir/blobs not referenced by any
// manifest under modelsDir/manifests. It refuses to guess: if the manifests
// are missing or any fails to parse it returns an error instead of a list, so
// a referenced blob can never be reported as an orphan.
func ollamaOrphans(modelsDir string, now time.Time) ([]string, error) {
	blobsDir := filepath.Join(modelsDir, "blobs")
	entries, err := os.ReadDir(blobsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	referenced, err := ollamaReferenced(filepath.Join(modelsDir, "manifests"))
	if err != nil {
		return nil, err
	}

	var orphans []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "sha256-") {
			continue
		}
		if strings.Contains(name, "-partial") {
			info, err := e.Info()
			if err != nil || now.Sub(info.ModTime()) < partialGrace {
				continue
			}
			orphans = append(orphans, filepath.Join(blobsDir, name))
			continue
		}
		if !referenced[name] {
			orphans = append(orphans, filepath.Join(blobsDir, name))
		}
	}
	return orphans, nil
}

type ollamaManifest struct {
	Config struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
}

// ollamaReferenced returns blob file names ("sha256-<hex>") named by manifests.
func ollamaReferenced(manifestsDir string) (map[string]bool, error) {
	if _, err := os.Stat(manifestsDir); err != nil {
		return nil, fmt.Errorf("ollama manifests unavailable, refusing to classify blobs: %w", err)
	}
	ref := make(map[string]bool)
	add := func(digest string) {
		if digest != "" {
			ref[strings.Replace(digest, ":", "-", 1)] = true
		}
	}
	err := filepath.WalkDir(manifestsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m ollamaManifest
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parse manifest %s: %w", path, err)
		}
		add(m.Config.Digest)
		for _, l := range m.Layers {
			add(l.Digest)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ref, nil
}

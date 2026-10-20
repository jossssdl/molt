// Package targets defines what molt considers reclaimable: build and
// dependency directories found while walking project trees (PatternTarget)
// and well-known cache locations outside any project (LocationTarget).
package targets

// Category groups targets for display and filtering.
type Category string

const (
	// CategoryDev covers per-project artifacts (node_modules, .venv, ...).
	CategoryDev Category = "dev"
	// CategoryAI covers local AI/ML caches (Hugging Face, Ollama, pip).
	CategoryAI Category = "ai"
)

// Target describes one kind of reclaimable artifact.
type Target interface {
	// Name is a short, stable identifier such as "node_modules".
	Name() string
	// Category groups the target.
	Category() Category
	// GitAware reports whether the parent repository state must be checked
	// before the artifact is considered safe to delete.
	GitAware() bool
}

// PatternTarget is discovered while walking a directory tree. Match is called
// for every directory; when it returns true the walker records the directory
// and does not descend into it.
type PatternTarget interface {
	Target
	// Match reports whether the directory at path (whose base name is name)
	// is an instance of this target. Implementations may stat siblings or
	// children to avoid false positives.
	Match(path, name string) bool
}

// LocationTarget lives at well-known paths and is resolved without walking.
type LocationTarget interface {
	Target
	// Resolve returns the existing files or directories to report. An empty
	// result with a nil error means the target is not present on this machine.
	Resolve() ([]string, error)
}

// Labeler is implemented by targets whose results deserve a human-readable
// name, such as "HF Model: org/name", instead of a bare path.
type Labeler interface {
	// Label returns the display name for a path produced by this target.
	Label(path string) string
}

// Default returns the built-in targets. AI caches are only included when
// includeAI is set, since they are machine-wide rather than per-project.
func Default(includeAI bool) []Target {
	ts := []Target{NodeModules(), PythonVenv(), RustTarget(), NextJS()}
	if includeAI {
		ts = append(ts, HuggingFace(), Pip(), OllamaOrphans())
	}
	return ts
}

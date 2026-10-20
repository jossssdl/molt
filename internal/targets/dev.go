package targets

import (
	"os"
	"path/filepath"
)

// patternTarget is a PatternTarget backed by a matcher function.
type patternTarget struct {
	name  string
	match func(path, name string) bool
}

func (t patternTarget) Name() string                 { return t.name }
func (patternTarget) Category() Category             { return CategoryDev }
func (patternTarget) GitAware() bool                 { return true }
func (t patternTarget) Match(path, name string) bool { return t.match(path, name) }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// siblingExists reports whether any of files exists next to path.
func siblingExists(path string, files ...string) bool {
	dir := filepath.Dir(path)
	for _, f := range files {
		if exists(filepath.Join(dir, f)) {
			return true
		}
	}
	return false
}

// NodeModules matches any directory named node_modules.
func NodeModules() Target {
	return patternTarget{
		name:  "node_modules",
		match: func(_, name string) bool { return name == "node_modules" },
	}
}

// PythonVenv matches .venv / venv directories that contain a pyvenv.cfg, which
// distinguishes real virtualenvs from unrelated folders with the same name.
func PythonVenv() Target {
	return patternTarget{
		name: ".venv",
		match: func(path, name string) bool {
			return (name == ".venv" || name == "venv") && exists(filepath.Join(path, "pyvenv.cfg"))
		},
	}
}

// RustTarget matches a "target" directory next to a Cargo.toml or pom.xml
// (Cargo and Maven build output). A bare "target" folder is ignored.
func RustTarget() Target {
	return patternTarget{
		name: "target",
		match: func(path, name string) bool {
			return name == "target" && siblingExists(path, "Cargo.toml", "pom.xml")
		},
	}
}

// NextJS matches a .next build directory next to a package.json.
func NextJS() Target {
	return patternTarget{
		name: ".next",
		match: func(path, name string) bool {
			return name == ".next" && siblingExists(path, "package.json")
		},
	}
}

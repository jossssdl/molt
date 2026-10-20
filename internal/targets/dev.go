package targets

import (
	"os"
	"path/filepath"
	"strings"
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

// siblingWithSuffix reports whether the directory containing path holds a
// regular entry whose name ends with suffix (for example ".tf").
func siblingWithSuffix(path, suffix string) bool {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), suffix) {
			return true
		}
	}
	return false
}

// SwiftBuild matches the SwiftPM ".build" directory next to a Package.swift.
func SwiftBuild() Target {
	return patternTarget{
		name: ".build",
		match: func(path, name string) bool {
			return name == ".build" && siblingExists(path, "Package.swift")
		},
	}
}

// GradleLocal matches the project-local ".gradle" and "build" directories next
// to a build.gradle or build.gradle.kts.
func GradleLocal() Target {
	return patternTarget{
		name: "gradle",
		match: func(path, name string) bool {
			return (name == ".gradle" || name == "build") &&
				siblingExists(path, "build.gradle", "build.gradle.kts")
		},
	}
}

// TerraformDir matches ".terraform" (downloaded providers and modules) next to
// Terraform sources (*.tf) or a dependency lock file.
func TerraformDir() Target {
	return patternTarget{
		name: ".terraform",
		match: func(path, name string) bool {
			return name == ".terraform" &&
				(siblingWithSuffix(path, ".tf") || siblingExists(path, ".terraform.lock.hcl"))
		},
	}
}

// CocoaPods matches the "Pods" directory next to a Podfile.
func CocoaPods() Target {
	return patternTarget{
		name: "Pods",
		match: func(path, name string) bool {
			return name == "Pods" && siblingExists(path, "Podfile")
		},
	}
}

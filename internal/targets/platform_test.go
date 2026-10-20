package targets

import (
	"os"
	"path/filepath"
	"testing"
)

// project creates a directory holding the given sibling files and the given
// child directory, and returns the child's path.
func project(t *testing.T, child string, siblings ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, s := range siblings {
		writeFile(t, filepath.Join(root, s), "x")
	}
	dir := filepath.Join(root, child)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func matches(tg Target, dir string) bool {
	return tg.(PatternTarget).Match(dir, filepath.Base(dir))
}

func TestPatternTargetsNeedTheirProjectFile(t *testing.T) {
	cases := []struct {
		name     string
		target   Target
		child    string
		siblings []string
		want     bool
	}{
		{"swift ok", SwiftBuild(), ".build", []string{"Package.swift"}, true},
		{"swift without Package.swift", SwiftBuild(), ".build", nil, false},
		{"swift wrong dir", SwiftBuild(), "build", []string{"Package.swift"}, false},

		{"gradle .gradle with groovy", GradleLocal(), ".gradle", []string{"build.gradle"}, true},
		{"gradle build with kotlin dsl", GradleLocal(), "build", []string{"build.gradle.kts"}, true},
		{"gradle build without build file", GradleLocal(), "build", []string{"settings.gradle"}, false},
		{"gradle unrelated dir", GradleLocal(), "dist", []string{"build.gradle"}, false},

		{"terraform with .tf", TerraformDir(), ".terraform", []string{"main.tf"}, true},
		{"terraform with lock file only", TerraformDir(), ".terraform", []string{".terraform.lock.hcl"}, true},
		{"terraform without sources", TerraformDir(), ".terraform", []string{"notes.txt"}, false},
		{"terraform tf must be a suffix", TerraformDir(), ".terraform", []string{"main.tf.bak"}, false},

		{"pods ok", CocoaPods(), "Pods", []string{"Podfile"}, true},
		{"pods without Podfile", CocoaPods(), "Pods", nil, false},
		{"pods wrong case", CocoaPods(), "pods2", []string{"Podfile"}, false},
	}
	for _, c := range cases {
		if got := matches(c.target, project(t, c.child, c.siblings...)); got != c.want {
			t.Errorf("%s: match = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTerraformSourceMustBeAFileNotADirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "modules.tf"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".terraform")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if matches(TerraformDir(), dir) {
		t.Error("a directory named *.tf must not count as Terraform sources")
	}
}

func TestProjectTargetsAreGitAwareDevTargets(t *testing.T) {
	for _, tg := range []Target{SwiftBuild(), GradleLocal(), TerraformDir(), CocoaPods()} {
		if !tg.GitAware() || tg.Category() != CategoryDev {
			t.Errorf("%s must be a git-aware dev target", tg.Name())
		}
		if _, ok := tg.(PatternTarget); !ok {
			t.Errorf("%s must be a PatternTarget", tg.Name())
		}
	}
	names := make(map[string]bool)
	for _, tg := range Default(false) {
		names[tg.Name()] = true
	}
	for _, want := range []string{".build", "gradle", ".terraform", "Pods"} {
		if !names[want] {
			t.Errorf("Default(false) is missing %q", want)
		}
	}
}

func TestXcodeDerivedDataOnlyOnMacOS(t *testing.T) {
	home := filepath.FromSlash("/h")
	want := filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData")
	if got := xcodeDerivedDataDir(home, "darwin"); got != want {
		t.Errorf("darwin: got %q, want %q", got, want)
	}
	for _, goos := range []string{"linux", "windows"} {
		if got := xcodeDerivedDataDir(home, goos); got != "" {
			t.Errorf("%s must not report DerivedData, got %q", goos, got)
		}
	}
	if xcodeDerivedDataDir("", "darwin") != "" {
		t.Error("no home, no DerivedData")
	}
}

func TestXcodeProjectsArePerProjectDirectories(t *testing.T) {
	dd := t.TempDir()
	hash := "abcdefghijklmnopqrstuvwxyzab" // 28 letters, like Xcode's
	for _, d := range []string{"MyApp-" + hash, "Other-" + hash, ".hidden"} {
		writeFile(t, filepath.Join(dd, d, "Build", "x"), "x")
	}
	writeFile(t, filepath.Join(dd, "loose-file"), "x")

	got := base(xcodeProjects(dd))
	if want := []string{"MyApp-" + hash, "Other-" + hash}; !equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if xcodeProjects("") != nil || xcodeProjects(filepath.Join(dd, "missing")) != nil {
		t.Error("empty or missing dir must yield nothing")
	}

	x := XcodeDerivedData()
	if x.Category() != CategoryDev || x.GitAware() {
		t.Error("Xcode DerivedData is a non-git dev location")
	}
	if l := x.(Labeler).Label(filepath.Join(dd, "MyApp-"+hash)); l != "Xcode DerivedData: MyApp" {
		t.Errorf("label = %q", l)
	}
	if l := x.(Labeler).Label(filepath.Join(dd, "Plain")); l != "Xcode DerivedData: Plain" {
		t.Errorf("label without hash = %q", l)
	}
}

func TestVllmCacheDirs(t *testing.T) {
	home := filepath.FromSlash("/h")
	got := vllmCacheDirs(envOf(map[string]string{"VLLM_CACHE_ROOT": "/v", "XDG_CACHE_HOME": "/x"}), home, "linux")
	want := []string{"/v", filepath.Join("/x", "vllm"), filepath.Join(home, ".cache", "vllm")}
	if !equal(got, want) {
		t.Errorf("linux: got %v, want %v", got, want)
	}
	if got := vllmCacheDirs(envOf(nil), home, "darwin"); len(got) != 1 || got[0] != filepath.Join(home, ".cache", "vllm") {
		t.Errorf("darwin: %v", got)
	}
	if got := vllmCacheDirs(envOf(map[string]string{"VLLM_CACHE_ROOT": "/v"}), home, "windows"); got != nil {
		t.Errorf("windows must report nothing, got %v", got)
	}
}

func TestMachineWideTargetsOnlyWithAICaches(t *testing.T) {
	has := func(ts []Target, name string) bool {
		for _, tg := range ts {
			if tg.Name() == name {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"vllm cache", "xcode derived"} {
		if has(Default(false), name) {
			t.Errorf("%s must be opt-in", name)
		}
		if !has(Default(true), name) {
			t.Errorf("%s missing from Default(true)", name)
		}
	}
}

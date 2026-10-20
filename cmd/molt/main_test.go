package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg, err := parseFlags(nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.dryRun {
		t.Error("-dry-run must default to true")
	}
	if cfg.minSize != 0 || cfg.minAge != 0 || len(cfg.exclude) != 0 || len(cfg.roots) != 0 ||
		cfg.json || cfg.list || cfg.ai || cfg.showVers {
		t.Errorf("unexpected non-zero defaults: %+v", cfg)
	}
}

func TestDryRunCanBeDisabledExplicitly(t *testing.T) {
	cfg, err := parseFlags([]string{"-dry-run=false"}, &bytes.Buffer{})
	if err != nil || cfg.dryRun {
		t.Fatalf("got dryRun=%v err=%v", cfg.dryRun, err)
	}
}

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{
		"-root", "a,b", "-root", "c",
		"-exclude", "x,y", "-exclude", "z",
		"-min-size", "100MB", "-min-age", "30d",
		"-workers", "3", "-ai-caches", "-list",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(cfg.roots, want) {
		t.Errorf("roots = %v, want %v", cfg.roots, want)
	}
	if want := []string{"x", "y", "z"}; !reflect.DeepEqual(cfg.exclude, want) {
		t.Errorf("exclude = %v, want %v", cfg.exclude, want)
	}
	if cfg.minSize != 100<<20 {
		t.Errorf("minSize = %d", cfg.minSize)
	}
	if cfg.minAge != 30*24*time.Hour {
		t.Errorf("minAge = %v", cfg.minAge)
	}
	if cfg.workers != 3 || !cfg.ai || !cfg.list {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestParseSize(t *testing.T) {
	ok := map[string]int64{
		"0": 0, "512": 512, "1k": 1 << 10, "100MB": 100 << 20, "1GB": 1 << 30,
		"1.5GB": 3 << 29, "2GiB": 2 << 30, " 10 mb ": 10 << 20, "1T": 1 << 40, "7B": 7,
	}
	for in, want := range ok {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "MB", "-5MB", "1XB"} {
		if _, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) should fail", in)
		}
	}
}

func TestParseAge(t *testing.T) {
	day := 24 * time.Hour
	ok := map[string]time.Duration{
		"30d": 30 * day, "2w": 14 * day, "12h": 12 * time.Hour, "90m": 90 * time.Minute,
		"7": 7 * day, "1.5d": 36 * time.Hour, " 3D ": 3 * day,
	}
	for in, want := range ok {
		if got, err := parseAge(in); err != nil || got != want {
			t.Errorf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "abc", "-1d", "10y"} {
		if _, err := parseAge(in); err == nil {
			t.Errorf("parseAge(%q) should fail", in)
		}
	}
}

func TestInvalidFlagValuesExitWithUsageError(t *testing.T) {
	for _, args := range [][]string{{"-min-size", "abc"}, {"-min-age", "soon"}, {"-workers", "-2"}, {"-nope"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("%v: exit %d, want 2 (stderr %q)", args, code, errb.String())
		}
		if errb.Len() == 0 {
			t.Errorf("%v: nothing reported on stderr", args)
		}
	}
}

func TestVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-version"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got, want := out.String(), "molt "+version+"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if version != "dev" {
		t.Errorf("default version = %q, want dev", version)
	}
}

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// project builds root/{big,small,skipme}/node_modules with known sizes.
func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "big", "node_modules", "f"), 5000)
	write(t, filepath.Join(root, "small", "node_modules", "f"), 10)
	write(t, filepath.Join(root, "skipme", "node_modules", "f"), 7000)
	return root
}

func runJSON(t *testing.T, args ...string) []jsonResult {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(append([]string{"-json"}, args...), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var res []jsonResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	return res
}

func projects(res []jsonResult) string {
	var p []string
	for _, r := range res {
		p = append(p, filepath.Base(filepath.Dir(r.Path)))
	}
	return strings.Join(p, ",")
}

func TestJSONOutputSortedBySize(t *testing.T) {
	res := runJSON(t, "-root", project(t))
	if got := projects(res); got != "skipme,big,small" {
		t.Fatalf("order = %s", got)
	}
	r := res[0]
	if r.Target != "node_modules" || r.Category != "dev" || r.SizeBytes != 7000 ||
		r.FileCount != 1 || r.Safety != "SAFE" {
		t.Errorf("unexpected entry %+v", r)
	}
}

func TestMinSizeFilter(t *testing.T) {
	if got := projects(runJSON(t, "-root", project(t), "-min-size", "1KB")); got != "skipme,big" {
		t.Fatalf("got %s", got)
	}
}

func TestExcludeFilter(t *testing.T) {
	root := project(t)
	if got := projects(runJSON(t, "-root", root, "-exclude", "skipme")); got != "big,small" {
		t.Errorf("by name: got %s", got)
	}
	if got := projects(runJSON(t, "-root", root, "-exclude", "skip*,small")); got != "big" {
		t.Errorf("glob+list: got %s", got)
	}
	if got := projects(runJSON(t, "-root", root, "-exclude", filepath.Join(root, "big"))); got != "skipme,small" {
		t.Errorf("by absolute path: got %s", got)
	}
}

func TestMinAgeFilterDropsFreshTargets(t *testing.T) {
	// Everything was just created, so a 30-day threshold removes it all.
	if res := runJSON(t, "-root", project(t), "-min-age", "30d"); len(res) != 0 {
		t.Fatalf("fresh targets must be filtered, got %s", projects(res))
	}
	// A zero threshold keeps them.
	if res := runJSON(t, "-root", project(t), "-min-age", "0d"); len(res) != 3 {
		t.Fatalf("got %d", len(res))
	}
}

func TestListOutput(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-list", "-root", project(t)}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	s := out.String()
	for _, want := range []string{"SIZE", "STATUS", "node_modules", "SAFE", "3 items", "safe to reclaim"} {
		if !strings.Contains(s, want) {
			t.Errorf("-list output missing %q:\n%s", want, s)
		}
	}
}

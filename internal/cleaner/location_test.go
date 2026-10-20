package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

// countingLocation is a LocationTarget that counts Resolve calls.
type countingLocation struct {
	paths *[]string
	calls *int
}

func (countingLocation) Name() string               { return "counting" }
func (countingLocation) Category() targets.Category { return targets.CategoryAI }
func (countingLocation) GitAware() bool             { return false }
func (c countingLocation) Resolve() ([]string, error) {
	*c.calls++
	return *c.paths, nil
}

func TestCleanAllResolvesLocationTargetOnce(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	var paths []string
	tgt := countingLocation{paths: &paths, calls: &calls}
	var items []scanner.ScanResult
	for i := 0; i < 50; i++ {
		p := filepath.Join(dir, fmt.Sprintf("blob%02d", i))
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		items = append(items, scanner.ScanResult{Path: p, Target: tgt, SizeBytes: 1})
	}
	outs := New(Options{DryRun: true}).CleanAll(context.Background(), items)
	if len(outs) != 50 {
		t.Fatalf("got %d outcomes", len(outs))
	}
	for _, o := range outs {
		if o.Err != nil || !o.DryRun {
			t.Fatalf("unexpected outcome %+v", o)
		}
	}
	if calls != 1 {
		t.Fatalf("Resolve called %d times for a 50-item batch, want 1", calls)
	}
}

func TestLocationOutsideResolvedSetIsRefused(t *testing.T) {
	calls := 0
	var paths []string
	p := filepath.Join(t.TempDir(), "stranger")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tgt := countingLocation{paths: &paths, calls: &calls}
	out := New(Options{}).Clean(context.Background(), scanner.ScanResult{Path: p, Target: tgt})
	if !errors.Is(out.Err, ErrUnsafe) || !exists(p) {
		t.Fatalf("want refusal: %+v", out)
	}
}

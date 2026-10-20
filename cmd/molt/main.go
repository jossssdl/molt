// Command molt scans for reclaimable development artifacts and local AI
// caches and reports whether each is safe to delete from Git's point of view.
//
// By default it opens an interactive TUI; -list or -json print a report
// instead. Deletion is a dry run unless -dry-run=false is given and the user
// confirms inside the TUI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jossssdl/molt/internal/cleaner"
	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
	"github.com/jossssdl/molt/internal/tui"
)

// rootsFlag collects repeated -root flags.
type rootsFlag []string

func (r *rootsFlag) String() string     { return strings.Join(*r, ",") }
func (r *rootsFlag) Set(v string) error { *r = append(*r, v); return nil }

func main() { os.Exit(run()) }

func run() int {
	var roots rootsFlag
	flag.Var(&roots, "root", "directory to scan for dev artifacts (repeatable; default: current directory)")
	ai := flag.Bool("ai-caches", false, "also scan AI caches (Hugging Face, pip, orphaned Ollama blobs)")
	workers := flag.Int("workers", 0, "number of measuring workers (default: number of CPUs)")
	asJSON := flag.Bool("json", false, "print results as JSON (no TUI)")
	list := flag.Bool("list", false, "print a table instead of opening the TUI")
	dryRun := flag.Bool("dry-run", true, "simulate deletions; use -dry-run=false to really delete")
	flag.Parse()

	if len(roots) == 0 && !*ai {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "molt:", err)
			return 1
		}
		roots = rootsFlag{cwd}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sc := &scanner.Scanner{
		Targets: targets.Default(*ai),
		Guard:   gitguard.NewExecGuard(),
		Workers: *workers,
	}

	if !*list && !*asJSON {
		c := cleaner.New(cleaner.Options{DryRun: *dryRun})
		if err := tui.Run(ctx, sc.Scan(ctx, roots), c, *dryRun); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "molt:", err)
			return 1
		}
		return 0
	}

	start := time.Now()
	var results []scanner.ScanResult
	for r := range sc.Scan(ctx, roots) {
		if r.Target == nil { // scan-level problem
			fmt.Fprintf(os.Stderr, "warning: %s: %v\n", r.Path, r.Err)
			continue
		}
		results = append(results, r)
	}
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "molt: interrupted, results are partial")
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].SizeBytes != results[j].SizeBytes {
			return results[i].SizeBytes > results[j].SizeBytes
		}
		return results[i].Path < results[j].Path
	})

	if *asJSON {
		return printJSON(results)
	}
	printTable(results, elapsed)
	return 0
}

func printTable(results []scanner.ScanResult, elapsed time.Duration) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SIZE\tSTATUS\tTARGET\tLAST COMMIT\tLAST MODIFIED\tPATH")
	var total, safe int64
	var safeCount int
	for _, r := range results {
		status := r.Safety().String()
		if d := r.Detail(); d != "" {
			status += " (" + d + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			scanner.FormatBytes(r.SizeBytes), status, r.Target.Name(),
			lastCommit(r), day(r.LastModified), r.Path)
		total += r.SizeBytes
		if r.Safety() == scanner.SafetySafe {
			safe += r.SizeBytes
			safeCount++
		}
	}
	tw.Flush()
	fmt.Printf("\n%d items, %s total, %s (%d items) safe to reclaim — scanned in %s\n",
		len(results), scanner.FormatBytes(total), scanner.FormatBytes(safe), safeCount,
		elapsed.Round(time.Millisecond))
}

func lastCommit(r scanner.ScanResult) string {
	if r.Git == nil || !r.Git.IsRepo {
		return "-"
	}
	return day(r.Git.LastCommit)
}

func day(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

type jsonResult struct {
	Path         string     `json:"path"`
	Target       string     `json:"target"`
	Category     string     `json:"category"`
	SizeBytes    int64      `json:"size_bytes"`
	FileCount    int64      `json:"file_count"`
	LastModified *time.Time `json:"last_modified,omitempty"`
	Safety       string     `json:"safety"`
	GitRepo      string     `json:"git_repo,omitempty"`
	LastCommit   *time.Time `json:"last_commit,omitempty"`
	Reasons      []string   `json:"reasons,omitempty"`
	Error        string     `json:"error,omitempty"`
}

func printJSON(results []scanner.ScanResult) int {
	out := make([]jsonResult, 0, len(results))
	for _, r := range results {
		j := jsonResult{
			Path:      r.Path,
			Target:    r.Target.Name(),
			Category:  string(r.Target.Category()),
			SizeBytes: r.SizeBytes,
			FileCount: r.FileCount,
			Safety:    r.Safety().String(),
		}
		if !r.LastModified.IsZero() {
			t := r.LastModified
			j.LastModified = &t
		}
		if g := r.Git; g != nil {
			j.GitRepo = g.Root
			j.Reasons = g.Reasons()
			if !g.LastCommit.IsZero() {
				t := g.LastCommit
				j.LastCommit = &t
			}
		}
		if r.Err != nil {
			j.Error = r.Err.Error()
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "molt:", err)
		return 1
	}
	return 0
}

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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jossssdl/molt/internal/cleaner"
	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
	"github.com/jossssdl/molt/internal/tui"
)

// version is overridden at build time:
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/molt
var version = "dev"

// listFlag collects repeated and comma-separated values.
type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// usageError marks a flag-parsing failure that has already been reported.
type usageError struct{ error }

// config is the parsed command line.
type config struct {
	roots    []string
	exclude  []string
	ai       bool
	workers  int
	json     bool
	list     bool
	dryRun   bool
	minSize  int64
	minAge   time.Duration
	showVers bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// parseFlags parses args without touching global state.
func parseFlags(args []string, stderr io.Writer) (config, error) {
	var (
		cfg     config
		roots   listFlag
		exclude listFlag
		minSize string
		minAge  string
	)
	fs := flag.NewFlagSet("molt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Var(&roots, "root", "directory to scan for dev artifacts (repeatable or comma-separated; default: current directory)")
	fs.Var(&exclude, "exclude", "folder name, glob or absolute path to skip (repeatable or comma-separated)")
	fs.BoolVar(&cfg.ai, "ai-caches", false, "also scan AI caches (Hugging Face models/datasets, pip, orphaned Ollama blobs)")
	fs.IntVar(&cfg.workers, "workers", 0, "number of measuring workers (default: number of CPUs)")
	fs.BoolVar(&cfg.json, "json", false, "print results as JSON (no TUI)")
	fs.BoolVar(&cfg.list, "list", false, "print a table instead of opening the TUI")
	fs.BoolVar(&cfg.dryRun, "dry-run", true, "simulate deletions; use -dry-run=false to really delete")
	fs.StringVar(&minSize, "min-size", "", "ignore targets smaller than this, e.g. 100MB or 1GB")
	fs.StringVar(&minAge, "min-age", "", "ignore targets active more recently than this, e.g. 30d, 2w or 12h")
	fs.BoolVar(&cfg.showVers, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return cfg, &usageError{err} // the flag package already printed it
	}
	var err error
	if minSize != "" {
		if cfg.minSize, err = parseSize(minSize); err != nil {
			return cfg, fmt.Errorf("invalid -min-size: %w", err)
		}
	}
	if minAge != "" {
		if cfg.minAge, err = parseAge(minAge); err != nil {
			return cfg, fmt.Errorf("invalid -min-age: %w", err)
		}
	}
	if cfg.workers < 0 {
		return cfg, errors.New("invalid -workers: must be >= 0")
	}
	cfg.roots, cfg.exclude = roots, exclude
	return cfg, nil
}

// parseSize understands "500", "100MB", "1.5GB", "2GiB". Units are binary
// (1KB = 1024 bytes) to match the sizes molt displays.
func parseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	units := []struct {
		suffix string
		mult   float64
	}{
		{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
		{"B", 1},
	}
	mult := 1.0
	for _, u := range units {
		if strings.HasSuffix(t, u.suffix) {
			t, mult = strings.TrimSpace(strings.TrimSuffix(t, u.suffix)), u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(t, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a size like 100MB or 1GB", s)
	}
	return int64(n * mult), nil
}

// parseAge understands "30d", "2w", "12h", "90m" and a bare number of days.
func parseAge(s string) (time.Duration, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	day := 24 * time.Hour
	units := map[string]time.Duration{"d": day, "w": 7 * day, "h": time.Hour, "m": time.Minute}
	mult := day // bare number = days
	if t != "" {
		if u, ok := units[t[len(t)-1:]]; ok {
			mult, t = u, t[:len(t)-1]
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a duration like 30d, 2w or 12h", s)
	}
	return time.Duration(n * float64(mult)), nil
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		var ue *usageError
		switch {
		case errors.Is(err, flag.ErrHelp):
			return 0
		case errors.As(err, &ue):
		default:
			fmt.Fprintln(stderr, "molt:", err)
		}
		return 2
	}
	if cfg.showVers {
		fmt.Fprintln(stdout, "molt", version)
		return 0
	}

	roots := cfg.roots
	if len(roots) == 0 && !cfg.ai {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "molt:", err)
			return 1
		}
		roots = []string{cwd}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sc := &scanner.Scanner{
		Targets: targets.Default(cfg.ai),
		Guard:   gitguard.NewExecGuard(),
		Workers: cfg.workers,
		Exclude: cfg.exclude,
		MinSize: cfg.minSize,
		MinAge:  cfg.minAge,
	}

	if !cfg.list && !cfg.json {
		c := cleaner.New(cleaner.Options{DryRun: cfg.dryRun})
		if err := tui.Run(ctx, sc.Scan(ctx, roots), c, cfg.dryRun); err != nil && ctx.Err() == nil {
			fmt.Fprintln(stderr, "molt:", err)
			return 1
		}
		return 0
	}

	start := time.Now()
	var results []scanner.ScanResult
	var skipped []scanner.ScanResult
	for r := range sc.Scan(ctx, roots) {
		if r.Target == nil { // scan-level problem
			skipped = append(skipped, r)
			continue
		}
		results = append(results, r)
	}
	reportSkipped(stderr, skipped)
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "molt: interrupted, results are partial")
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].SizeBytes != results[j].SizeBytes {
			return results[i].SizeBytes > results[j].SizeBytes
		}
		return results[i].Path < results[j].Path
	})

	if cfg.json {
		return printJSON(stdout, stderr, results)
	}
	printTable(stdout, results, elapsed)
	return 0
}

// reportSkipped prints a compact summary of paths the scan could not read.
func reportSkipped(w io.Writer, skipped []scanner.ScanResult) {
	if len(skipped) == 0 {
		return
	}
	fmt.Fprintf(w, "warning: %d paths skipped (permission/error)\n", len(skipped))
	for i, r := range skipped {
		if i == 3 {
			fmt.Fprintf(w, "  ... and %d more\n", len(skipped)-3)
			break
		}
		fmt.Fprintf(w, "  %s: %v\n", r.Path, r.Err)
	}
}

func printTable(w io.Writer, results []scanner.ScanResult, elapsed time.Duration) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SIZE\tSTATUS\tTARGET\tLAST COMMIT\tLAST MODIFIED\tPATH")
	var total, safe int64
	var safeCount int
	for _, r := range results {
		status := r.Safety().String()
		if d := r.Detail(); d != "" {
			status += " (" + d + ")"
		}
		name := r.Target.Name()
		if l := r.Label(); l != "" {
			name = l
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			scanner.FormatBytes(r.SizeBytes), status, name,
			lastCommit(r), day(r.LastModified), r.Path)
		total += r.SizeBytes
		if r.Safety() == scanner.SafetySafe {
			safe += r.SizeBytes
			safeCount++
		}
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%d items, %s total, %s (%d items) safe to reclaim — scanned in %s\n",
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
	Label        string     `json:"label,omitempty"`
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

func printJSON(stdout, stderr io.Writer, results []scanner.ScanResult) int {
	out := make([]jsonResult, 0, len(results))
	for _, r := range results {
		j := jsonResult{
			Path:      r.Path,
			Label:     r.Label(),
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
			j.Reasons = g.Details()
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
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(stderr, "molt:", err)
		return 1
	}
	return 0
}

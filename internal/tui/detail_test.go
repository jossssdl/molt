package tui

import (
	"strings"
	"testing"

	"github.com/jossssdl/molt/internal/gitguard"
	"github.com/jossssdl/molt/internal/scanner"
	"github.com/jossssdl/molt/internal/targets"
)

func TestDetailPanelExplainsExactGitReasons(t *testing.T) {
	g := &gitguard.GitStatus{IsRepo: true, Root: "/repo", DirtyFiles: 2, UntrackedFiles: 1, Unpushed: 4, NoUpstream: true}
	it := scanner.ScanResult{Path: "/repo/node_modules", Target: targets.NodeModules(), Git: g}
	_, why, _ := detailLines(it)
	for _, want := range []string{"DIRTY GIT", "2 tracked files have uncommitted changes", "1 untracked file", "4 commits not pushed", "no upstream"} {
		if !strings.Contains(why, want) {
			t.Errorf("detail %q missing %q", why, want)
		}
	}
}

func TestLabelShownInRowAndDetail(t *testing.T) {
	it := scanner.ScanResult{Path: "/c/hub/models--meta-llama--Llama-3.2-3B", Target: targets.HuggingFace(), SizeBytes: 10}
	if got := rowName(it, 60); got != "HF Model: meta-llama/Llama-3.2-3B" {
		t.Errorf("rowName = %q", got)
	}
	if label, _, meta := detailLines(it); label == "" || !strings.Contains(meta, "HF Model") {
		t.Errorf("label=%q meta=%q", label, meta)
	}
}

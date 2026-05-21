package diffviewer

import (
	"strings"
	"testing"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/x/ansi"
)

func TestUpdateIgnoresStaleDiffContentMsg(t *testing.T) {
	m := New(false, "auto")
	m.vp.SetWidth(120)
	key := cacheKey("/", false)
	m.cache[key] = &cachedNode{}
	m.renderID = 2

	updated, _ := m.Update(diffContentMsg{
		cacheKey: key,
		text:     "stale",
		renderID: 1,
	})

	if updated.cache[key].diff != "" {
		t.Fatalf("expected stale render to be ignored, got %q", updated.cache[key].diff)
	}
}

func TestUpdateAcceptsCurrentDiffContentMsg(t *testing.T) {
	m := New(false, "auto")
	m.vp.SetWidth(120)
	key := cacheKey("/", false)
	m.cache[key] = &cachedNode{}
	m.renderID = 3

	updated, _ := m.Update(diffContentMsg{
		cacheKey: key,
		text:     "fresh",
		renderID: 3,
	})

	if updated.cache[key].diff != "fresh" {
		t.Fatalf("expected current render to be applied, got %q", updated.cache[key].diff)
	}
}

func TestSetFilePatchRerendersEmptyCachedEntry(t *testing.T) {
	m := New(false, "dark")
	m.Common.Width = 120
	file := &gitdiff.File{NewName: "src/app.go"}
	key := cacheKey("src/app.go", false)
	m.cache[key] = &cachedNode{
		path:  "src/app.go",
		files: []*gitdiff.File{file},
	}

	updated, cmd := m.SetFilePatch(file)

	if cmd == nil {
		t.Fatal("expected empty cached file diff to trigger a new render")
	}
	if updated.file == nil || updated.file.path != "src/app.go" {
		t.Fatalf("expected selected file to be src/app.go, got %#v", updated.file)
	}
}

func TestSetDirPatchRerendersEmptyCachedEntry(t *testing.T) {
	m := New(false, "dark")
	m.Common.Width = 120
	file := &gitdiff.File{NewName: "src/app.go"}
	key := cacheKey("src", false)
	m.cache[key] = &cachedNode{
		path:  "src",
		files: []*gitdiff.File{file},
	}

	updated, cmd := m.SetDirPatch("src", []*gitdiff.File{file})

	if cmd == nil {
		t.Fatal("expected empty cached dir diff to trigger a new render")
	}
	if updated.dir == nil || updated.dir.path != "src" {
		t.Fatalf("expected selected dir to be src, got %#v", updated.dir)
	}
}

func TestRenderPreamble_Empty(t *testing.T) {
	if got := renderPreamble(""); got != "" {
		t.Fatalf("expected empty string for empty preamble, got %q", got)
	}
	if got := renderPreamble("   \n  \n  "); got != "" {
		t.Fatalf("expected empty string for whitespace-only preamble, got %q", got)
	}
}

func TestRenderPreamble_GitShow(t *testing.T) {
	preamble := `commit abc123def456
Author: Jane Doe <jane@example.com>
Date:   Mon Jan 1 00:00:00 2026 +0000

    feat: add new feature

    This is the body of the commit message.`

	got := renderPreamble(preamble)
	plain := ansi.Strip(got)

	// All original content lines should be preserved in the output.
	for _, want := range []string{
		"commit abc123def456",
		"Author: Jane Doe <jane@example.com>",
		"Date:   Mon Jan 1 00:00:00 2026 +0000",
		"feat: add new feature",
		"This is the body of the commit message.",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, plain)
		}
	}
}

func TestRenderPreamble_MergeCommit(t *testing.T) {
	preamble := `commit abc123def456
Merge: aaa111 bbb222
Author: Jane Doe <jane@example.com>
Date:   Mon Jan 1 00:00:00 2026 +0000

    Merge branch 'feature' into main`

	got := renderPreamble(preamble)
	plain := ansi.Strip(got)

	for _, want := range []string{
		"Merge: aaa111 bbb222",
		"Merge branch 'feature' into main",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, plain)
		}
	}
}

package diffviewer

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

func TestDeltaArgsCapsUnifiedLineLength(t *testing.T) {
	args := deltaArgs(120, false, nil, nil)

	if strings.Contains(strings.Join(args, "\x00"), "--max-line-length=0") {
		t.Fatalf("unified delta args must not disable long-line truncation: %#v", args)
	}
	if !containsArg(args, "--max-line-length=4096") {
		t.Fatalf("expected unified delta args to cap long lines, got %#v", args)
	}
	if containsArg(args, "--side-by-side") {
		t.Fatalf("did not expect side-by-side arg for unified render: %#v", args)
	}
}

func TestDeltaArgsCapsSideBySideLineLength(t *testing.T) {
	args := deltaArgs(120, true, nil, nil)

	if !containsArg(args, "--max-line-length=120") {
		t.Fatalf("expected side-by-side delta args to cap long lines, got %#v", args)
	}
	if !containsArg(args, "--side-by-side") {
		t.Fatalf("expected side-by-side arg, got %#v", args)
	}
}

func TestSetFilePatchCancelsInFlightRender(t *testing.T) {
	m := New(false, "dark")
	m.Common.Width = 120

	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	var closeFirstStarted sync.Once
	var closeFirstCanceled sync.Once
	var calls int
	var callsMu sync.Mutex
	m.renderer.run = func(
		ctx context.Context,
		_ []string,
		writeInput func(io.Writer) error,
	) ([]byte, error) {
		if err := writeInput(io.Discard); err != nil {
			return nil, err
		}
		callsMu.Lock()
		calls++
		call := calls
		callsMu.Unlock()
		if call == 1 {
			closeFirstStarted.Do(func() { close(firstStarted) })
			<-ctx.Done()
			closeFirstCanceled.Do(func() { close(firstCanceled) })
			return nil, ctx.Err()
		}
		return []byte("fresh"), nil
	}

	m, firstCmd := m.SetFilePatch(&gitdiff.File{NewName: "one.go"})
	if firstCmd == nil {
		t.Fatal("expected first render command")
	}
	firstDone := make(chan tea.Msg, 1)
	go func() {
		firstDone <- firstCmd()
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first render to start")
	}

	m, secondCmd := m.SetFilePatch(&gitdiff.File{NewName: "two.go"})
	if secondCmd == nil {
		t.Fatal("expected second render command")
	}

	select {
	case <-firstCanceled:
	case <-time.After(time.Second):
		t.Fatal("expected second render to cancel first render")
	}
	if msg := <-firstDone; msg != nil {
		t.Fatalf("expected canceled render to return nil msg, got %#v", msg)
	}
	msg := secondCmd()
	if msg == nil {
		t.Fatal("expected second render message")
	}
}

func TestSetFilePatchCacheHitCancelsInFlightRender(t *testing.T) {
	m := New(false, "dark")
	m.Common.Width = 120

	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	m.renderer.run = func(
		ctx context.Context,
		_ []string,
		writeInput func(io.Writer) error,
	) ([]byte, error) {
		if err := writeInput(io.Discard); err != nil {
			return nil, err
		}
		close(firstStarted)
		<-ctx.Done()
		close(firstCanceled)
		return nil, ctx.Err()
	}

	m, firstCmd := m.SetFilePatch(&gitdiff.File{NewName: "one.go"})
	if firstCmd == nil {
		t.Fatal("expected first render command")
	}
	firstDone := make(chan tea.Msg, 1)
	go func() {
		firstDone <- firstCmd()
	}()

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first render to start")
	}

	file := &gitdiff.File{NewName: "two.go"}
	key := cacheKey("two.go", false)
	m.cache[key] = &cachedNode{
		path:  "two.go",
		files: []*gitdiff.File{file},
		diff:  "cached",
		ready: true,
	}
	m, cachedCmd := m.SetFilePatch(file)
	if cachedCmd != nil {
		t.Fatal("expected cached file selection to avoid a new render")
	}
	if m.file == nil || m.file.diff != "cached" {
		t.Fatalf("expected cached diff to become active, got %#v", m.file)
	}

	select {
	case <-firstCanceled:
	case <-time.After(time.Second):
		t.Fatal("expected cache hit to cancel in-flight render")
	}
	if msg := <-firstDone; msg != nil {
		t.Fatalf("expected canceled render to return nil msg, got %#v", msg)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
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

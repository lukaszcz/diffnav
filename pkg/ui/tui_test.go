package ui

import (
	"image/color"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/dlvhdr/diffnav/pkg/config"
)

func TestSearchUpdateEnterWithNoResultsDoesNotPanic(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.searching = true
	m.search.Focus()
	m.search.SetValue("does-not-match")
	m.setSearchResults()

	updated, _ := m.searchUpdate(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	if updated.searching {
		t.Fatal("expected search to stop after pressing enter")
	}
	if updated.resultsCursor != 0 {
		t.Fatalf("expected cursor to remain at 0, got %d", updated.resultsCursor)
	}
}

func TestSearchUpdateKeepsCursorValidWhenResultsAreEmpty(t *testing.T) {
	m := newTestMainModel(t)
	m.searching = true
	m.search.Focus()
	m.filtered = nil
	m.resultsCursor = 0

	updated, _ := m.searchUpdate(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if updated.resultsCursor != 0 {
		t.Fatalf(
			"expected cursor to remain at 0 after down on empty results, got %d",
			updated.resultsCursor,
		)
	}

	updated.resultsCursor = -3
	updated.search.SetValue("does-not-match")
	updated.setSearchResults()
	if updated.resultsCursor != 0 {
		t.Fatalf("expected cursor to clamp to 0 for empty results, got %d", updated.resultsCursor)
	}
}

func TestSearchResultsRenderWhenFileTreeIsHidden(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = false
	m.searching = true
	m.search.SetWidth(m.searchWidth())
	m.setSearchResults()
	m.resultsVp.SetWidth(m.config.UI.SearchTreeWidth)
	m.resultsVp.SetHeight(m.mainContentHeight() - searchHeight)
	m.resultsVp.SetContent(m.resultsView())

	view := m.View().Content
	if !strings.Contains(view, "yarn.lock") {
		t.Fatal("expected search results to be visible even when the file tree is hidden")
	}
}

func TestHiddenTreeSearchEnterThenToggleDoesNotPanic(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40

	m = updateMainModel(t, m, tea.KeyPressMsg(tea.Key{Text: "e", Code: 'e'}))
	m = updateMainModel(t, m, tea.KeyPressMsg(tea.Key{Text: "t", Code: 't'}))
	m = updateMainModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = updateMainModel(t, m, tea.KeyPressMsg(tea.Key{Text: "e", Code: 'e'}))

	if !m.isShowingFileTree {
		t.Fatal("expected file tree to be visible after toggling it back on")
	}
	if m.search.Width() < 0 {
		t.Fatalf("expected non-negative search width, got %d", m.search.Width())
	}
	_ = m.View().Content
}

func TestHiddenTreeSearchClickNearLeftEdgeDoesNotShowFileTree(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = false
	m.searching = true

	updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if result.isShowingFileTree {
		t.Fatal("expected left-edge click during hidden-tree search to leave the file tree hidden")
	}
}

func TestHiddenSidebarGrabStillShowsFileTreeWhenNotSearching(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = false
	m.searching = false

	updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 1, Button: tea.MouseLeft}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if !result.isShowingFileTree {
		t.Fatal("expected left-edge click on the hidden sidebar grab line to show the file tree")
	}
}

// Regression: clicking the first column of the diff viewer (one column right
// of the hidden sidebar's grab line) must not reopen the file tree — the
// sidebar grab zone must not extend into diff-viewer columns or selection
// starting just past the divider becomes impossible.
func TestHiddenSidebarGrabDoesNotConsumeDiffViewerClicks(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = false
	m.searching = false

	updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if result.isShowingFileTree {
		t.Fatal(
			"expected click one column right of the hidden grab line to fall through to the diff viewer",
		)
	}
	if result.draggingSidebar {
		t.Fatal(
			"expected click one column right of the hidden grab line to not start a sidebar drag",
		)
	}
}

// Regression: clicking one or two columns to the right of the "│" divider
// between the file tree and the diff viewer must start a diff selection
// rather than initiating a sidebar resize drag.
func TestDiffViewerClickJustRightOfDividerDoesNotStartDragging(t *testing.T) {
	for _, offset := range []int{1, 2} {
		m := newTestMainModel(t)
		m.width = 160
		m.height = 40
		m.isShowingFileTree = true
		m.searching = false

		x := m.sidebarWidth() + offset
		updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: 1, Button: tea.MouseLeft}))

		result, ok := updated.(mainModel)
		if !ok {
			t.Fatalf("offset=%d: unexpected model type %T", offset, updated)
		}
		if result.draggingSidebar {
			t.Fatalf(
				"offset=%d: expected click %d col(s) right of divider to not start a sidebar drag",
				offset,
				offset,
			)
		}
	}
}

func TestSearchSidebarBorderClickDoesNotStartDragging(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = true
	m.searching = true
	m.fileTree.SetSize(m.config.UI.FileTreeWidth, m.mainContentHeight()-searchHeight)

	updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{
		X:      m.sidebarWidth(),
		Y:      1,
		Button: tea.MouseLeft,
	}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if result.draggingSidebar {
		t.Fatal("expected sidebar dragging to stay disabled while searching")
	}
}

func TestSearchSidebarDragMotionIsIgnored(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.isShowingFileTree = true
	m.searching = true
	m.draggingSidebar = true
	m.fileTree.SetSize(m.config.UI.FileTreeWidth, m.mainContentHeight()-searchHeight)

	updated, _ := m.handleMouse(tea.MouseMotionMsg(tea.Mouse{
		X:      40,
		Y:      1,
		Button: tea.MouseLeft,
	}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if result.draggingSidebar {
		t.Fatal("expected search-mode drag motion to clear dragging state")
	}
	if result.fileTree.Width() != m.fileTree.Width() {
		t.Fatalf(
			"expected file tree width to remain %d, got %d",
			m.fileTree.Width(),
			result.fileTree.Width(),
		)
	}
}

func TestBackgroundColorDetectionStillWorksWhileSearching(t *testing.T) {
	m := newTestMainModel(t)
	m.searching = true
	m.search.Focus()
	m.themeOverride = nil
	m.isDarkBackground = nil

	updated := updateMainModel(t, m, tea.BackgroundColorMsg{
		Color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
	})

	if updated.isDarkBackground == nil {
		t.Fatal("expected background color detection to set theme state while searching")
	}
	if *updated.isDarkBackground {
		t.Fatal("expected light background detection while searching")
	}
}

func TestThemeDetectionTimeoutFallsBackToDark(t *testing.T) {
	m := newTestMainModel(t)
	m.themeOverride = nil
	m.isDarkBackground = nil

	updated := updateMainModel(t, m, themeDetectTimeoutMsg{})
	if updated.isDarkBackground == nil {
		t.Fatal("expected timeout to resolve theme state")
	}
	if !*updated.isDarkBackground {
		t.Fatal("expected timeout fallback to dark background")
	}
}

func TestLateBackgroundDetectionIgnoredAfterTimeout(t *testing.T) {
	m := newTestMainModel(t)
	m.themeOverride = nil
	m.isDarkBackground = nil
	m = updateMainModel(t, m, themeDetectTimeoutMsg{})

	updated := updateMainModel(t, m, tea.BackgroundColorMsg{
		Color: color.RGBA{R: 255, G: 255, B: 255, A: 255},
	})
	if updated.isDarkBackground == nil {
		t.Fatal("expected theme state to remain resolved")
	}
	if !*updated.isDarkBackground {
		t.Fatal("expected late background message to be ignored after timeout")
	}
}

// TestRightSideSelectionEndToEnd drives the full mouse pipeline against a
// real delta render to verify a click + drag on the right half of an SBS
// view actually produces a visible highlight. This is a regression guard
// against the production failure mode the user reported ("right side
// selection doesn't work, the selection doesn't even appear").
func TestRightSideSelectionEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("delta"); err != nil {
		t.Skip("delta not installed, skipping end-to-end test")
	}
	m := newTestMainModel(t)
	isDark := true
	m.isDarkBackground = &isDark
	m.diffViewer.SetDarkBackground(true)
	m.fileTree.SetDarkBackground(true)
	m.sideBySide = true
	m.diffViewer.SetSideBySide(true)

	// Size the UI like a typical wide terminal. WindowSizeMsg triggers the
	// dir-diff render through diff() which returns a Cmd that runs delta.
	m = updateMainModel(t, m, tea.WindowSizeMsg{Width: 160, Height: 40})
	// Run the deferred fetchFileTree Cmd to populate m.files / m.fileTree
	// the way Init() would in production, then trigger SetDirPatch.
	m = updateMainModel(t, m, m.fetchFileTree())

	// Drain the diff Cmd(s) until a diffContentMsg lands in diffViewer
	// (delta runs in a goroutine via the Cmd indirection).
	deadline := time.Now().Add(3 * time.Second)
	for m.diffViewer.GutterCol() <= 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for diffContentMsg; gutterCol still %d",
				m.diffViewer.GutterCol())
		}
		// Resolve any pending Cmds we know about.
		if cmd := m.diffViewer.Init(); cmd != nil {
			if msg := cmd(); msg != nil {
				m = updateMainModel(t, m, msg)
			}
		}
		// Best-effort: re-trigger a render so the dir delta call resolves.
		m.diffViewer.ClearCache()
		if cmd := m.diffViewer.SetSize(
			m.width-m.sidebarWidth(),
			m.mainContentHeight(),
		); cmd != nil {
			if msg := cmd(); msg != nil {
				m = updateMainModel(t, m, msg)
			}
		}
	}

	gutter := m.diffViewer.GutterCol()
	if gutter <= 0 {
		t.Fatalf("expected gutterCol > 0 after dir-diff render, got %d", gutter)
	}

	// Pick mouse coordinates on the right half of the diff pane. The diff
	// pane starts at x=sidebarWidth and the gutter is at +gutter inside it.
	rightX := m.sidebarWidth() + gutter + 5
	clickY := m.headerHeight() + 1 + diffviewerDirHeaderHeight() + 1

	// View() must run once before any mouse events: bubblezone registers
	// zones during zone.Scan inside View(), and zone.Get* lookups return
	// empty info until then.
	_ = m.View().Content

	m = updateMainModel(t, m, tea.MouseClickMsg(tea.Mouse{
		X:      rightX,
		Y:      clickY,
		Button: tea.MouseLeft,
	}))
	if !m.diffViewer.IsSelecting() {
		t.Fatalf(
			"expected diffViewer.IsSelecting()==true after click at (x=%d,y=%d)",
			rightX,
			clickY,
		)
	}
	m = updateMainModel(t, m, tea.MouseMotionMsg(tea.Mouse{
		X:      rightX + 10,
		Y:      clickY,
		Button: tea.MouseLeft,
	}))

	view := m.View().Content
	if !strings.Contains(view, "\x1b[7m") {
		t.Fatalf(
			"expected reverse-video escape (\\x1b[7m) in View() after right-side drag — selection rendered nothing",
		)
	}
}

// Tiny helper so the test doesn't need to import diffviewer just for the
// header height constant.
func diffviewerDirHeaderHeight() int { return 3 }

// Enter on a highlighted file should launch $EDITOR and skip the
// directory-toggle path the filetree pane uses for Enter on dirs.
func TestEnterOnFileOpensEditor(t *testing.T) {
	t.Setenv("EDITOR", "true")
	m := newTestMainModel(t)
	m.activePanel = FileTreePanel
	m.fileTree.SetCursorByPath("yarn.lock")

	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command (editor exec) when Enter hits a file with $EDITOR set")
	}
	// The cursor must still be on the same file; pressing Enter on a file
	// must not toggle/collapse anything in the tree.
	if got := result.fileTree.CurrNodePath(); got != "yarn.lock" {
		t.Fatalf("expected cursor to remain on yarn.lock, got %q", got)
	}
}

// Without $EDITOR set, Enter on a file must be a no-op rather than falling
// through to the directory-toggle behavior.
func TestEnterOnFileWithoutEditorIsNoop(t *testing.T) {
	t.Setenv("EDITOR", "")
	m := newTestMainModel(t)
	m.activePanel = FileTreePanel
	m.fileTree.SetCursorByPath("yarn.lock")

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if got := result.fileTree.CurrNodePath(); got != "yarn.lock" {
		t.Fatalf("expected cursor to remain on yarn.lock, got %q", got)
	}
}

// Regression: Enter on a directory still toggles it (the new file-open
// behavior must only apply to file nodes).
func TestEnterOnDirectoryStillToggles(t *testing.T) {
	m := newTestMainModel(t)
	m.activePanel = FileTreePanel
	m.fileTree.SetCursorByPath("graphql-server/tests")

	dirNode := m.fileTree.GetCurrNode()
	if dirNode == nil {
		t.Fatal("expected to find graphql-server/tests directory node")
	}
	wasOpen := dirNode.IsOpen()

	updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}

	toggled := result.fileTree.GetCurrNode()
	if toggled == nil {
		t.Fatal("expected current node after toggle")
	}
	if toggled.IsOpen() == wasOpen {
		t.Fatalf(
			"expected dir open state to toggle from %v, got %v",
			wasOpen,
			toggled.IsOpen(),
		)
	}
}

func newTestMainModel(t *testing.T) mainModel {
	t.Helper()
	zone.NewGlobal()

	cfg := config.DefaultConfig()
	data, err := os.ReadFile("../../examples/multiple_files.txt")
	if err != nil {
		t.Fatal(err)
	}

	files, _, err := gitdiff.Parse(strings.NewReader(string(data) + "\n"))
	if err != nil {
		t.Fatal(err)
	}

	m := New(string(data), cfg)
	m.files = files
	m.fileTree = m.fileTree.SetFiles(files)

	return m
}

func updateMainModel(t *testing.T, m mainModel, msg tea.Msg) mainModel {
	t.Helper()

	updated, _ := m.Update(msg)
	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}

	return result
}

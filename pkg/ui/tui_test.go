package ui

import (
	"image/color"
	"os"
	"strings"
	"testing"

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
	if updated.search.Focused() {
		t.Fatal("expected search input to blur after pressing enter")
	}
	if updated.search.Value() != "" {
		t.Fatalf("expected search input to clear after pressing enter, got %q", updated.search.Value())
	}
	if updated.resultsCursor != 0 {
		t.Fatalf("expected cursor to remain at 0, got %d", updated.resultsCursor)
	}
}

func TestSearchUpdateEscClearsAndBlursSearch(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.searching = true
	m.search.Focus()
	m.search.SetValue("query")
	m.setSearchResults()

	updated, _ := m.searchUpdate(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))

	if updated.searching {
		t.Fatal("expected search to stop after pressing escape")
	}
	if updated.search.Focused() {
		t.Fatal("expected search input to blur after pressing escape")
	}
	if updated.search.Value() != "" {
		t.Fatalf("expected search input to clear after pressing escape, got %q", updated.search.Value())
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
		t.Fatalf("expected cursor to remain at 0 after down on empty results, got %d", updated.resultsCursor)
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

	updated, _ := m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseLeft}))

	result, ok := updated.(mainModel)
	if !ok {
		t.Fatalf("unexpected model type %T", updated)
	}
	if !result.isShowingFileTree {
		t.Fatal("expected left-edge click on the hidden sidebar grab line to show the file tree")
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
		t.Fatalf("expected file tree width to remain %d, got %d", m.fileTree.Width(), result.fileTree.Width())
	}
}

func TestWindowResizeStillUpdatesLayoutWhileSearching(t *testing.T) {
	m := newTestMainModel(t)
	m.width = 100
	m.height = 40
	m.searching = true
	m.search.Focus()
	m.search.SetWidth(m.searchWidth())
	m.diffViewer.Width = 10
	m.diffViewer.Height = 10

	updated := updateMainModel(t, m, tea.WindowSizeMsg{Width: 132, Height: 48})

	if updated.width != 132 || updated.height != 48 {
		t.Fatalf("expected window size to update to 132x48, got %dx%d", updated.width, updated.height)
	}
	if updated.search.Width() != updated.searchWidth() {
		t.Fatalf("expected search width to update to %d, got %d", updated.searchWidth(), updated.search.Width())
	}
	wantDiffWidth := 132 - updated.sidebarWidth()
	if updated.diffViewer.Width != wantDiffWidth {
		t.Fatalf("expected diff viewer width to update to %d, got %d", wantDiffWidth, updated.diffViewer.Width)
	}
	if updated.diffViewer.Height != updated.mainContentHeight() {
		t.Fatalf("expected diff viewer height to update to %d, got %d", updated.mainContentHeight(), updated.diffViewer.Height)
	}
}

func TestPasteStillUpdatesSearchInputWhileSearching(t *testing.T) {
	m := newTestMainModel(t)
	m.searching = true
	m.search.Focus()
	m.setSearchResults()

	updated := updateMainModel(t, m, tea.PasteMsg{Content: "yarn"})

	if got := updated.search.Value(); got != "yarn" {
		t.Fatalf("expected pasted search value %q, got %q", "yarn", got)
	}
	if len(updated.filtered) != 1 || updated.filtered[0] != "yarn.lock" {
		t.Fatalf("expected pasted search value to narrow results to yarn.lock, got %#v", updated.filtered)
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

func TestLateBackgroundDetectionOverridesTimeoutFallback(t *testing.T) {
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
	if *updated.isDarkBackground {
		t.Fatal("expected late background message to replace the timeout fallback")
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

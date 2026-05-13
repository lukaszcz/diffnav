package diffviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// (1) Single-line selection: ansi.Strip of the spliced output contains the
// expected substring.
func TestSpliceReverse_SingleLineContainsSubstring(t *testing.T) {
	line := "hello world"
	w := lipgloss.Width(line)

	out := spliceReverse(line, 6, 11, w)

	plain := ansi.Strip(out)
	if !strings.Contains(plain, "world") {
		t.Fatalf("expected stripped output to contain %q, got %q", "world", plain)
	}
	if !strings.Contains(out, "\x1b[7m") || !strings.Contains(out, "\x1b[27m") {
		t.Fatalf("expected SGR 7 / 27 in output, got %q", out)
	}
}

// (2) Multi-line selection: highlightRange + clampToBand produce correct
// ranges for first / interior / last lines.
func TestHighlightRange_MultiLineWithBandClamp(t *testing.T) {
	start := Point{Line: 2, Col: 5}
	end := Point{Line: 4, Col: 8}
	lineWidth := 40
	band := [2]int{0, 30}

	// First line: from start.Col to lineWidth.
	a, b := highlightRange(2, start, end, lineWidth)
	if a != 5 || b != 40 {
		t.Fatalf("first line pre-clamp: want (5, 40), got (%d, %d)", a, b)
	}
	a, b = clampToBand(a, b, band)
	if a != 5 || b != 30 {
		t.Fatalf("first line clamped: want (5, 30), got (%d, %d)", a, b)
	}

	// Interior line: [0, lineWidth).
	a, b = highlightRange(3, start, end, lineWidth)
	if a != 0 || b != 40 {
		t.Fatalf("interior pre-clamp: want (0, 40), got (%d, %d)", a, b)
	}
	a, b = clampToBand(a, b, band)
	if a != 0 || b != 30 {
		t.Fatalf("interior clamped: want (0, 30), got (%d, %d)", a, b)
	}

	// Last line: [0, end.Col).
	a, b = highlightRange(4, start, end, lineWidth)
	if a != 0 || b != 8 {
		t.Fatalf("last line pre-clamp: want (0, 8), got (%d, %d)", a, b)
	}
	a, b = clampToBand(a, b, band)
	if a != 0 || b != 8 {
		t.Fatalf("last line clamped: want (0, 8), got (%d, %d)", a, b)
	}
}

// (2 cont.) Single-line selection through highlightRange.
func TestHighlightRange_SingleLine(t *testing.T) {
	start := Point{Line: 5, Col: 3}
	end := Point{Line: 5, Col: 12}

	a, b := highlightRange(5, start, end, 80)
	if a != 3 || b != 12 {
		t.Fatalf("single line: want (3, 12), got (%d, %d)", a, b)
	}
}

// (3) Reverse drag (anchor below head) normalizes correctly.
func TestSelection_NormalizedReverseDrag(t *testing.T) {
	s := selection{
		anchor: Point{Line: 7, Col: 4},
		head:   Point{Line: 2, Col: 10},
	}
	start, end := s.normalized()
	if start != (Point{Line: 2, Col: 10}) {
		t.Fatalf("start: want {2 10}, got %+v", start)
	}
	if end != (Point{Line: 7, Col: 4}) {
		t.Fatalf("end: want {7 4}, got %+v", end)
	}
}

// (3 cont.) Same-line reverse drag: anchor.Col > head.Col normalizes by col.
func TestSelection_NormalizedSameLineReverse(t *testing.T) {
	s := selection{
		anchor: Point{Line: 3, Col: 20},
		head:   Point{Line: 3, Col: 5},
	}
	start, end := s.normalized()
	if start != (Point{Line: 3, Col: 5}) || end != (Point{Line: 3, Col: 20}) {
		t.Fatalf("normalize same-line reverse: got start=%+v end=%+v", start, end)
	}
}

// (3 cont.) Forward drag is a no-op.
func TestSelection_NormalizedForwardDrag(t *testing.T) {
	s := selection{
		anchor: Point{Line: 1, Col: 1},
		head:   Point{Line: 3, Col: 3},
	}
	start, end := s.normalized()
	if start != s.anchor || end != s.head {
		t.Fatalf("forward drag should be unchanged: got start=%+v end=%+v", start, end)
	}
}

// (6) ANSI round-trip — single line: ansi.Strip(spliceReverse(line, a, b, w))
// == ansi.Strip(line). Catches leaking SGR state.
func TestSpliceReverse_AnsiRoundTripSingleLine(t *testing.T) {
	// Styled line: red "foo", default " bar", green "baz".
	line := "\x1b[31mfoo\x1b[0m bar \x1b[32mbaz\x1b[0m"
	w := lipgloss.Width(line)

	out := spliceReverse(line, 2, 7, w)

	if got, want := ansi.Strip(out), ansi.Strip(line); got != want {
		t.Fatalf("ansi-strip mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// (6 cont.) ANSI round-trip — multi-line: for every line in the selected
// range, ansi.Strip(spliced) == ansi.Strip(original). This catches SGR
// state that delta opens on one line and (unusually) doesn't re-open on the
// next — our \x1b[27m reset must not leak across the join.
func TestSpliceReverse_AnsiRoundTripMultiLine(t *testing.T) {
	lines := []string{
		"\x1b[31mfirst line content\x1b[0m",
		"\x1b[32msecond line of stuff\x1b[0m",
		"\x1b[33mthird and final line\x1b[0m",
	}
	start := Point{Line: 0, Col: 6}
	end := Point{Line: 2, Col: 5}
	band := [2]int{0, 1 << 30}

	for i, line := range lines {
		w := lipgloss.Width(line)
		a, b := highlightRange(i, start, end, w)
		a, b = clampToBand(a, b, band)
		a, b = clampToLine(a, b, w)
		spliced := spliceReverse(line, a, b, w)
		if got, want := ansi.Strip(spliced), ansi.Strip(line); got != want {
			t.Fatalf("line %d: ansi-strip mismatch:\n  got:  %q\n  want: %q", i, got, want)
		}
	}
}

// (6 cont.) ANSI round-trip with no actual selection on a line (a >= b
// would normally be skipped by callers — but spliceReverse itself should
// still preserve content). We exercise the normal non-empty path here and
// trust the View() short-circuit elsewhere.
func TestSpliceReverse_EmptyAndPlainLine(t *testing.T) {
	line := "plain text without ansi"
	w := lipgloss.Width(line)
	out := spliceReverse(line, 6, 10, w)
	if got, want := ansi.Strip(out), line; got != want {
		t.Fatalf("plain line round-trip:\n  got:  %q\n  want: %q", got, want)
	}
}

// (4) Click-without-drag leaves no selection: StartSelection followed by
// EndSelection with anchor unchanged returns (_, false) and HasSelection
// remains false.
func TestSelection_ClickWithoutDragLeavesNoSelection(t *testing.T) {
	m := New(false, "auto")
	m.StartSelection(Point{Line: 3, Col: 5})
	if !m.IsSelecting() {
		t.Fatalf("expected IsSelecting() == true immediately after StartSelection")
	}

	text, ok := m.EndSelection()
	if ok {
		t.Fatalf("expected ok == false for click-without-drag, got text=%q", text)
	}
	if text != "" {
		t.Fatalf("expected empty text, got %q", text)
	}
	if m.HasSelection() {
		t.Fatalf("expected HasSelection() == false after click-without-drag")
	}
	if m.IsSelecting() {
		t.Fatalf("expected IsSelecting() == false after EndSelection")
	}
}

// (5) Selection survives viewport scroll: the highlighted screen row shifts
// by the scroll delta and the underlying content is unchanged.
func TestSelection_SurvivesViewportScroll(t *testing.T) {
	m := New(false, "auto")
	m.vp.SetWidth(40)
	m.vp.SetHeight(5)

	var b strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "line-%02d-some-content\n", i)
	}
	m.vp.SetContent(strings.TrimSuffix(b.String(), "\n"))

	// Single-line selection on content line 7 covering "line-07".
	m.StartSelection(Point{Line: 7, Col: 0})
	m.ExtendSelection(Point{Line: 7, Col: 7})

	// YOffset=5 → visible content rows 5..9 → line 7 at screen row 2.
	m.ScrollDown(5)
	view1 := m.View()

	// YOffset=6 → visible content rows 6..10 → line 7 at screen row 1.
	m.ScrollDown(1)
	view2 := m.View()

	findHighlight := func(label, s string) (int, string) {
		for i, l := range strings.Split(s, "\n") {
			if strings.Contains(l, "\x1b[7m") {
				return i, l
			}
		}
		t.Fatalf("%s: no highlighted row found in:\n%s", label, s)
		return -1, ""
	}

	row1, line1 := findHighlight("view1", view1)
	row2, line2 := findHighlight("view2", view2)

	if row2 != row1-1 {
		t.Fatalf("expected highlight row to shift up by 1 after ScrollDown(1): row1=%d row2=%d", row1, row2)
	}
	// Trailing scrollbar character may differ between views (it tracks scroll
	// position); compare only the content portion that came from the viewport.
	if !strings.Contains(ansi.Strip(line1), "line-07") {
		t.Fatalf("view1: expected highlighted row to contain %q, got %q", "line-07", ansi.Strip(line1))
	}
	if !strings.Contains(ansi.Strip(line2), "line-07") {
		t.Fatalf("view2: expected highlighted row to contain %q, got %q", "line-07", ansi.Strip(line2))
	}
}

// (8) Column detection on a known delta side-by-side render fixture.
// Expect gutterCol close to width/2 and stable across content lines.
func TestDetectGutterCol_DeltaSideBySideFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "sbs_render_w120.ansi"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	stripped := ansi.Strip(string(raw))

	const width = 120
	got := detectGutterCol(stripped, width)

	target := width / 2
	if absInt(got-target) > 10 {
		t.Fatalf("gutterCol=%d not within ±10 of width/2=%d", got, target)
	}

	// Stability: every content line (≥3 dividers) must place its center
	// '│' at the same column the detector picked.
	contentLines := 0
	for _, line := range strings.Split(stripped, "\n") {
		positions := visualColumnsOf(line, '│')
		if len(positions) < 3 {
			continue
		}
		contentLines++
		nearest := positions[0]
		for _, p := range positions {
			if absInt(p-target) < absInt(nearest-target) {
				nearest = p
			}
		}
		if nearest != got {
			t.Fatalf("line %q: nearest-to-midpoint %d != detected %d", line, nearest, got)
		}
	}
	if contentLines < 2 {
		t.Fatalf("fixture only has %d content lines; detector input is too thin", contentLines)
	}
}

// (14) Wide-character gutter detection. Feed a side-by-side render with CJK
// and emoji to the left of the gutter; assert gutterCol (visual column)
// lands on the actual '│' divider, not on a rune-index that skewed left.
func TestDetectGutterCol_WideCharsToLeftOfGutter(t *testing.T) {
	// Hand-construct lines where the center '│' sits at visual column 30,
	// with CJK / emoji to its left. Each line has ≥3 '│' so it passes the
	// skip-line gate. The runes-to-the-left would skew a rune-index-based
	// detector by 6+ columns; a visual-column detector must see col 30.
	//
	// Layout per line:
	//   '│'  cols 0
	//   '  1 '  cols 1..4
	//   '│'  col 5
	//   CJK/emoji block (each rune width 2) + padding to reach col 30
	//   '│'  col 30  <-- center divider
	//   right-side filler + '│' at the right border
	//
	// We pick width=60 so target=30 aligns with the planted center column.
	pad := func(width int) string { return strings.Repeat(" ", width) }
	// Left side: 24 visual cols of content after the post-LN '│' (col 5),
	// putting the center '│' at col 5+1+24 = 30.
	leftBlocks := []string{
		"日本語テスト" + pad(12),       // 6 CJK * 2 = 12 visual, +12 spaces = 24
		"こんにちは世界" + pad(10),       // 7 CJK * 2 = 14 visual, +10 spaces = 24
		"🐶🐱🐭🐹" + pad(16),       // 4 emoji * 2 = 8 visual, +16 spaces = 24
	}
	rightFiller := pad(24)
	lines := make([]string, 0, len(leftBlocks))
	for _, lb := range leftBlocks {
		// "│" + "  1 " + "│" + leftBlock(24) + "│" + rightFiller(24) + "│"
		lines = append(lines, "│"+"  1 "+"│"+lb+"│"+rightFiller+"│")
	}

	// Sanity: every line must have the center '│' at visual col 30.
	for _, l := range lines {
		positions := visualColumnsOf(l, '│')
		if len(positions) < 3 {
			t.Fatalf("line missing dividers (have %d): %q", len(positions), l)
		}
		found := false
		for _, p := range positions {
			if p == 30 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected a '│' at visual col 30; positions=%v line=%q", positions, l)
		}
	}

	got := detectGutterCol(strings.Join(lines, "\n"), 60)
	if got != 30 {
		t.Fatalf("gutterCol: want 30 (visual col of center '│'), got %d", got)
	}
}

// (15) Tabs in diff content: delta's current config expands tabs to spaces in
// the output we read from m.file.diff. This test is a TRIPWIRE — if a future
// delta version or config option ships raw tabs, the col↔visual-column math
// in the selection layer (visualColumnsOf, ansi.Cut, etc.) needs revisiting
// because a single '\t' rune occupies multiple visual columns and is not
// width-1 like the rest of our coordinate math assumes.
func TestDeltaOutput_NoRawTabs_Tripwire(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "sbs_render_w120.ansi"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	stripped := ansi.Strip(string(raw))
	// The input fixture (sbs_input.diff) contains literal '\t' indentation,
	// so a delta that *didn't* expand tabs would surface them here.
	if strings.ContainsRune(stripped, '\t') {
		t.Fatalf("delta output unexpectedly contains raw '\\t' — tab expansion is off; selection col math needs revisiting")
	}
}

// (16) First-N robustness: a diff whose first 10 content lines mix hunk-
// header decorations (which have <3 '│' and are skipped by the gate) and
// real diff rows still converges on the same gutterCol as a clean run.
func TestDetectGutterCol_HunkHeaderRobustness(t *testing.T) {
	// "Clean" content line: '│' at cols 0, 5, 30, 55.
	pad := func(n int) string { return strings.Repeat(" ", n) }
	clean := "│" + "  1 " + "│" + pad(24) + "│" + pad(24) + "│"
	// Hunk-header / decoration lines: 0 or 1 '│', skipped by the gate.
	headers := []string{
		"───┐",
		"42: │", // 1 '│' — exactly at the gate's lower bound, still skipped.
		"───┘",
		"@@ -1,8 +1,9 @@",
	}

	// Reference: a "clean run" with only content lines.
	cleanRun := strings.Repeat(clean+"\n", 4)
	want := detectGutterCol(cleanRun, 60)
	if want != 30 {
		t.Fatalf("sanity: clean run want gutter at 30, got %d", want)
	}

	// Mixed: 4 header decorations interleaved with 4 content lines, well
	// within the first-10-content-lines window.
	mixed := strings.Join([]string{
		headers[0],
		headers[1],
		clean,
		headers[2],
		clean,
		headers[3],
		clean,
		clean,
	}, "\n")
	got := detectGutterCol(mixed, 60)
	if got != want {
		t.Fatalf("mixed-headers run: got %d, want %d (same as clean run)", got, want)
	}
}

// View() returns the original viewport output unchanged when no selection
// is active and none is finalized. Guards the common-path short-circuit.
func TestView_NoSelectionNoOverlay(t *testing.T) {
	m := New(false, "auto")
	m.vp.SetWidth(40)
	m.vp.SetHeight(5)
	m.vp.SetContent("alpha\nbeta\ngamma")

	out := m.View()
	if strings.Contains(out, "\x1b[7m") || strings.Contains(out, "\x1b[27m") {
		t.Fatalf("expected no SGR-reverse escapes when sel.active==false && sel.has==false, got:\n%q", out)
	}
}

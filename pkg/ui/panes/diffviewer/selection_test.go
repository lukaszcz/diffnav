package diffviewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"charm.land/lipgloss/v2"
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

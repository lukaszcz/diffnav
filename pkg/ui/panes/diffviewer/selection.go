package diffviewer

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// Point identifies a position within the rendered diff content.
//
// Col is a *visual column* — the same coordinate space as paneX and
// ansi.Cut/lipgloss.Width — never a rune index or byte offset. Callers must
// translate any rune-index-derived value into visual columns before storing
// it in a Point.
type Point struct {
	Line int
	Col  int
}

// selection captures an in-progress or finalized text selection in the diff
// viewport. colBand is [lo, hi) — half-open — matching the conventions used
// throughout highlightRange and ansi.Cut.
type selection struct {
	anchor  Point
	head    Point
	colBand [2]int
	active  bool
	has     bool
}

// normalized returns (start, end) with start <= end in (line, col) order.
func (s selection) normalized() (Point, Point) {
	a, b := s.anchor, s.head
	if a.Line > b.Line || (a.Line == b.Line && a.Col > b.Col) {
		a, b = b, a
	}
	return a, b
}

// highlightRange returns the half-open [a, b) visual-column range to
// highlight on a given content line, before colBand and line-width clamps.
//
// Rules (from PLAN.md):
//   - Single-line selection:    a, b = start.Col, end.Col
//   - First line of multi-line: a, b = start.Col, lineWidth
//   - Interior line:            a, b = 0,         lineWidth
//   - Last line:                a, b = 0,         end.Col
func highlightRange(line int, start, end Point, lineWidth int) (int, int) {
	if start.Line == end.Line {
		return start.Col, end.Col
	}
	switch {
	case line == start.Line:
		return start.Col, lineWidth
	case line == end.Line:
		return 0, end.Col
	default:
		return 0, lineWidth
	}
}

// clampToBand clips [a, b) to the selection's colBand [lo, hi). Used for
// multi-line selections so interior lines don't extend beyond the active
// column.
func clampToBand(a, b int, band [2]int) (int, int) {
	lo, hi := band[0], band[1]
	if a < lo {
		a = lo
	}
	if b > hi {
		b = hi
	}
	if a > b {
		a = b
	}
	return a, b
}

// clampToLine clips b to min(b, lineWidth). MUST be applied after
// clampToBand — protects against drag-right-past-EOL when the selection's
// end column exceeds the actual rendered line width.
func clampToLine(a, b, lineWidth int) (int, int) {
	if b > lineWidth {
		b = lineWidth
	}
	if a > lineWidth {
		a = lineWidth
	}
	if a > b {
		a = b
	}
	return a, b
}

// spliceReverse returns line with the visual column range [a, b) wrapped in
// SGR 7 (reverse video) / SGR 27 (reset). Uses ansi.Cut so ANSI escapes and
// wide characters are preserved.
func spliceReverse(line string, a, b, lineWidth int) string {
	return ansi.Cut(line, 0, a) + "\x1b[7m" + ansi.Cut(line, a, b) + "\x1b[27m" + ansi.Cut(line, b, lineWidth)
}

// visualColumnsOf walks s rune-by-rune accumulating runewidth.RuneWidth and
// returns the visual column at which each occurrence of r begins.
//
// Operates in visual-column space (not rune index) so that CJK / emoji to
// the left of the matched rune are accounted for. The returned columns are
// in the same coordinate space as paneX and ansi.Cut.
func visualColumnsOf(s string, r rune) []int {
	var out []int
	col := 0
	for _, c := range s {
		if c == r {
			out = append(out, col)
		}
		col += runewidth.RuneWidth(c)
	}
	return out
}

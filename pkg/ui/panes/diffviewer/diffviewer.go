package diffviewer

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/diffnav/pkg/config"
	"github.com/dlvhdr/diffnav/pkg/filenode"
	"github.com/dlvhdr/diffnav/pkg/icons"
	"github.com/dlvhdr/diffnav/pkg/ui/common"
	"github.com/dlvhdr/diffnav/pkg/utils"
)

// DirHeaderHeight is the height of the header band rendered above the
// diff viewport. Exported so callers translating mouse coordinates into
// content rows (see tui.go's diffPanePoint) can skip past it.
const DirHeaderHeight = 3

type cachedNode struct {
	path      string
	files     []*gitdiff.File
	additions int64
	deletions int64
	diff      string
}

type nodeCache map[string]*cachedNode

func cacheKey(path string, sideBySide bool) string {
	if sideBySide {
		return path + ":sbs"
	}
	return path
}

type Model struct {
	common.Common
	vp    viewport.Model
	file  *cachedNode
	dir   *cachedNode
	cache nodeCache
	// Monotonic render generation used to drop stale async results.
	renderID   uint64
	sideBySide bool
	themeMode  themeMode
	// nil means unknown (leave delta behavior unchanged).
	isDarkBackground *bool
	preamble         string
	sel              selection
	// gutterCol is the visual column of the side-by-side center divider.
	// -1 means "no column constraint" (unified mode or not yet detected).
	gutterCol int
	// leftContentCol / rightContentCol are the first visual column inside
	// each side's content area (i.e. just past the post-line-number "│"
	// border). -1 means "undetected" — selections then fall back to the
	// full half band starting at 0 / gutterCol+1.
	leftContentCol  int
	rightContentCol int
}

// SetPreamble stores the preamble text (e.g. commit metadata from git show).
func (m *Model) SetPreamble(preamble string) {
	m.preamble = preamble
}

type themeMode uint8

const (
	themeAuto themeMode = iota
	themeLight
	themeDark
)

func New(sideBySide bool, theme string) Model {
	m := Model{
		vp:              viewport.Model{},
		sideBySide:      sideBySide,
		cache:           map[string]*cachedNode{},
		themeMode:       parseThemeMode(theme),
		gutterCol:       -1,
		leftContentCol:  -1,
		rightContentCol: -1,
	}
	switch m.themeMode {
	case themeLight:
		isDark := false
		m.isDarkBackground = &isDark
	case themeDark:
		isDark := true
		m.isDarkBackground = &isDark
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "j", "n":
			break
		case "up", "k", "N", "p":
			break
		default:
			vp, vpCmd := m.vp.Update(msg)
			cmds = append(cmds, vpCmd)
			m.vp = vp
		}

	case diffContentMsg:
		if msg.renderID != m.renderID {
			break
		}
		// Clear before gutter detection so a mid-drag content swap leaves
		// sel.active == false; the next MouseMotionMsg is a no-op until the
		// next click.
		m.sel = selection{}
		// Wrap long lines so unified delta output (which delta does not wrap
		// itself) folds at the viewport edge instead of overflowing. The wrap
		// uses delta's wrap-left-symbol '↵', so joinWrappedLines collapses
		// the resulting rows back to one logical line on selection/copy.
		diff := wrapLongLines(msg.text, m.vp.Width())
		if _, ok := m.cache[msg.cacheKey]; ok {
			m.cache[msg.cacheKey].diff = diff
		}
		m.vp.SetContent(diff)
		m.refreshColumnDetection(diff)
	}

	return m, tea.Batch(cmds...)
}

// refreshColumnDetection re-runs gutter and per-side content column detection
// on the supplied diff content (typically the freshly rendered or cached
// payload). Keeping this in one place ensures every code path that swaps the
// viewport content also refreshes the selection bands — without this, loading
// a cached file or toggling between unified and side-by-side via cached
// content would leave stale column metadata in place and make the right side
// effectively unselectable.
//
// All three values stay at -1 unless we can detect *both* a gutter divider
// AND the post-line-number borders on each side. Delta renders new/deleted
// files as unified even when --side-by-side is requested, which would
// otherwise leave gutterCol stuck at the vpWidth/2 fallback and split every
// selection in half over empty space.
//
// Robustness: a defer/recover guard wraps the parse so a future delta layout
// change can't crash the whole TUI — on any panic we leave the columns at
// -1 and the selection falls back to the unified [0, MaxInt) band.
func (m *Model) refreshColumnDetection(content string) {
	m.gutterCol = -1
	m.leftContentCol = -1
	m.rightContentCol = -1
	if !m.sideBySide || m.vp.Width() <= 0 {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Warn("diffviewer: column detection panicked; falling back to unified selection band", "panic", r)
			m.gutterCol = -1
			m.leftContentCol = -1
			m.rightContentCol = -1
		}
	}()
	stripped := ansi.Strip(content)
	g := detectGutterCol(stripped, m.vp.Width())
	l, r := detectSideContentCols(stripped, g)
	// Strict plausibility check — beyond the obvious negative-value guards,
	// reject any layout where the detected columns don't form a coherent
	// (leadingBorder=0 < leftContent < gutter < rightContent < vpWidth)
	// sequence. Anything outside that window means delta produced something
	// we don't understand; treat it as "unparseable" rather than risk
	// clamping selections to nonsense ranges.
	w := m.vp.Width()
	if g <= 0 || g >= w || l <= 0 || l >= g || r <= g || r >= w {
		return
	}
	m.gutterCol = g
	m.leftContentCol = l
	m.rightContentCol = r
}

const scrollbarWidth = 3 // 1 space + 1 scrollbar character + 1 padding

func (m Model) View() string {
	vpView := m.vp.View()
	if m.sel.active || m.sel.has {
		vpView = m.applyHighlight(vpView)
	}
	scrollbar := common.RenderScrollbar(m.vp.Height(), m.vp.TotalLineCount(), m.vp.YOffset())
	if scrollbar != "" {
		vpView = lipgloss.JoinHorizontal(lipgloss.Top, vpView, " ", scrollbar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), vpView)
}

// applyHighlight overlays SGR-reverse video on the visible viewport rows that
// fall inside the active or finalized selection. Returns the input unchanged
// when the selection is fully off-screen.
//
// A defer/recover guard protects the live render path from any panic that a
// malformed line or unexpected delta output could trigger inside the slice
// and ansi.Cut operations below — on failure the unhighlighted view is
// returned so the TUI keeps working.
func (m Model) applyHighlight(vpView string) (out string) {
	out = vpView
	defer func() {
		if r := recover(); r != nil {
			log.Warn("diffviewer: applyHighlight panicked; rendering unhighlighted view", "panic", r)
			out = vpView
		}
	}()
	start, end := m.sel.normalized()
	top := m.vp.YOffset()
	bottom := top + m.vp.Height()
	if end.Line < top || start.Line >= bottom {
		return vpView
	}
	visible := strings.Split(vpView, "\n")
	for screenRow, line := range visible {
		contentLine := top + screenRow
		if contentLine < start.Line || contentLine > end.Line {
			continue
		}
		w := lipgloss.Width(line)
		a, b := highlightRange(contentLine, start, end, w)
		a, b = clampToBand(a, b, m.sel.colBand)
		a, b = clampToLine(a, b, w)
		if a >= b {
			continue
		}
		visible[screenRow] = spliceReverse(line, a, b, w)
	}
	return strings.Join(visible, "\n")
}

func (m *Model) SetSize(width, height int) tea.Cmd {
	m.Common.Width = width
	m.Common.Height = height
	m.vp.SetWidth(m.contentWidth())
	m.vp.SetHeight(m.Common.Height - DirHeaderHeight)
	m.ClearCache()
	return m.diff()
}

func (m Model) contentWidth() int {
	return m.Common.Width - scrollbarWidth
}

// Height returns the diff viewport's visible row count (excluding the
// dir-header band). Mouse-edge logic uses this to detect dragging past the
// pane edge.
func (m *Model) Height() int {
	return m.vp.Height()
}

// Width returns the diff viewport's content column count (excluding the
// scrollbar gutter).
func (m *Model) Width() int {
	return m.vp.Width()
}

func (m *Model) diff() tea.Cmd {
	if m.themeMode == themeAuto && m.isDarkBackground == nil {
		return nil
	}

	if m.file != nil {
		key := cacheKey(m.file.path, m.sideBySide)
		if cached, ok := m.cache[key]; ok && cached.diff != "" {
			m.file = cached
			m.vp.SetContent(cached.diff)
			m.refreshColumnDetection(cached.diff)
			return nil
		}
		node := &cachedNode{
			path:      m.file.path,
			files:     m.file.files,
			additions: m.file.additions,
			deletions: m.file.deletions,
		}
		m.file = node
		m.cache[key] = node
		m.renderID++
		return diffFile(node, m.contentWidth(), m.sideBySide, m.deltaThemeArgs(), m.renderID)
	} else if m.dir != nil {
		key := cacheKey(m.dir.path, m.sideBySide)
		if cached, ok := m.cache[key]; ok && cached.diff != "" {
			m.dir = cached
			m.vp.SetContent(cached.diff)
			m.refreshColumnDetection(cached.diff)
			return nil
		}
		node := &cachedNode{
			path:      m.dir.path,
			files:     m.dir.files,
			additions: m.dir.additions,
			deletions: m.dir.deletions,
		}
		m.dir = node
		m.cache[key] = node
		m.renderID++
		preamble := ""
		if m.dir.path == "/" {
			preamble = m.preamble
		}
		return diffDir(
			node,
			m.contentWidth(),
			m.sideBySide,
			m.deltaThemeArgs(),
			common.SelectionColor(common.Selected, m.isDarkBackground),
			preamble,
			m.renderID,
		)
	}

	return nil
}

func (m Model) headerView() string {
	if m.dir != nil {
		return m.dirHeaderView()
	}

	if m.file == nil || len(m.file.files) != 1 {
		return ""
	}
	name := m.file.path
	base := lipgloss.NewStyle()

	fileIcon := icons.GetIcon(name, false)
	prefix := base.Render(fileIcon) + base.Render(" ")
	name = utils.TruncateString(name, m.Common.Width-lipgloss.Width(prefix))
	top := prefix + base.Bold(true).Render(name)

	bottom := filenode.ViewFileDiffStats(m.file.files[0], base)

	return base.
		Width(m.Common.Width).
		Height(DirHeaderHeight - 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("8")).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, bottom))
}

func (m Model) dirHeaderView() string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Blue)
	prefix := base.Render(" ")
	name := utils.TruncateString(m.dir.path, m.Common.Width-lipgloss.Width(prefix))

	top := prefix + base.Bold(true).Render(name)
	bottom := filenode.ViewDiffStats(m.dir.additions, m.dir.deletions, base)
	return base.
		Width(m.Common.Width).
		Height(DirHeaderHeight - 1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(lipgloss.Color("8")).
		Render(lipgloss.JoinVertical(lipgloss.Left, top, bottom))
}

func (m Model) SetFilePatch(file *gitdiff.File) (Model, tea.Cmd) {
	m.sel = selection{}
	m.dir = nil

	fname := filenode.GetFileName(file)
	key := cacheKey(fname, m.sideBySide)
	if cached, ok := m.cache[key]; ok {
		m.file = cached
		m.vp.SetContent(cached.diff)
		m.refreshColumnDetection(cached.diff)
		return m, nil
	}

	files := make([]*gitdiff.File, 1)
	files[0] = file
	additions, deletions := filenode.DiffStats(file)
	m.file = &cachedNode{
		path:      fname,
		files:     files,
		additions: additions,
		deletions: deletions,
	}
	m.cache[key] = m.file

	cmd := m.diff()
	return m, cmd
}

func (m Model) SetDirPatch(dirPath string, files []*gitdiff.File) (Model, tea.Cmd) {
	m.sel = selection{}
	m.file = nil

	key := cacheKey(dirPath, m.sideBySide)
	if cached, ok := m.cache[key]; ok {
		m.dir = cached
		m.vp.SetContent(cached.diff)
		m.refreshColumnDetection(cached.diff)
		return m, nil
	}

	var added, deleted int64
	for _, file := range files {
		na, nd := filenode.DiffStats(file)
		added += na
		deleted += nd
	}
	m.dir = &cachedNode{
		path:      dirPath,
		files:     files,
		additions: added,
		deletions: deleted,
	}
	m.cache[key] = m.dir
	cmd := m.diff()
	return m, cmd
}

func (m *Model) GoToTop() {
	m.vp.GotoTop()
}

// SetSideBySide updates the diff view mode and re-renders.
func (m *Model) SetSideBySide(sideBySide bool) tea.Cmd {
	m.sideBySide = sideBySide
	return m.diff()
}

// ScrollUp scrolls the viewport up by the given number of lines.
func (m *Model) ScrollUp(lines int) {
	m.vp.ScrollUp(lines)
}

// ScrollDown scrolls the viewport down by the given number of lines.
func (m *Model) ScrollDown(lines int) {
	m.vp.ScrollDown(lines)
}

// ScrollBottom scrolls the viewport to the bottom.
func (m *Model) ScrollBottom() {
	m.vp.GotoBottom()
}

// ScrollTop scrolls the viewport to its top.
func (m *Model) ScrollTop() {
	m.vp.GotoTop()
}

// YOffset returns the viewport's current top content row.
func (m *Model) YOffset() int {
	return m.vp.YOffset()
}

// StartSelection begins a new selection anchored at p. Derives colBand from
// p.Col, m.gutterCol, and the detected per-side content columns. The band
// excludes the leading border, line numbers, and post-LN border on whichever
// side the click landed.
//
// Lines without the standard side-by-side layout — hunk-header boxes
// ("60: struct …"), filename decorations, separators — opt out of side-
// specific clamping so the user can select content that lives outside the
// post-line-number borders. Sets active=true, has=false. Any prior selection
// is replaced.
func (m *Model) StartSelection(p Point) {
	var lo, hi int
	sbs := m.gutterCol > 0 && m.isSBSContentLine(p.Line)
	switch {
	case !sbs:
		// Unified mode, undetected, or click on a non-SBS line (hunk header,
		// filename decoration, separator): no column constraint.
		lo, hi = 0, math.MaxInt
	case p.Col > m.gutterCol:
		lo = m.gutterCol + 1
		if m.rightContentCol > lo {
			lo = m.rightContentCol
		}
		hi = math.MaxInt
	default:
		// Left side (p.Col < gutterCol) or directly on the divider — snap left.
		lo = 0
		if m.leftContentCol > 0 {
			lo = m.leftContentCol
		}
		hi = m.gutterCol
	}
	if p.Col < lo {
		p.Col = lo
	}
	if p.Col >= hi {
		p.Col = hi - 1
	}
	m.sel = selection{
		anchor:  p,
		head:    p,
		colBand: [2]int{lo, hi},
		active:  true,
	}
}

// isSBSContentLine reports whether the cached diff line at `line` matches the
// side-by-side structure (leading "│" border at col 0). Hunk-header boxes,
// filename decorations, separators, and other delta chrome do not start with
// "│" and are treated as plain text for selection clamping purposes.
func (m *Model) isSBSContentLine(line int) bool {
	if line < 0 {
		return false
	}
	var src string
	switch {
	case m.file != nil:
		src = m.file.diff
	case m.dir != nil:
		src = m.dir.diff
	}
	if src == "" {
		return false
	}
	lines := strings.Split(src, "\n")
	if line >= len(lines) {
		return false
	}
	return strings.HasPrefix(ansi.Strip(lines[line]), "│")
}

// ExtendSelection moves the selection head to p, clamping p.Col into the
// active colBand. The line axis is never clamped — vertical drag spans the
// full content. No-op when no drag is active.
func (m *Model) ExtendSelection(p Point) {
	if !m.sel.active {
		return
	}
	lo, hi := m.sel.colBand[0], m.sel.colBand[1]
	if p.Col < lo {
		p.Col = lo
	}
	if p.Col >= hi {
		// colBand is half-open [lo, hi); the last valid head column is hi-1.
		p.Col = hi - 1
	}
	m.sel.head = p
}

// EndSelection finalizes an active selection. If the head never moved away
// from the anchor it returns ("", false) with the selection cleared.
// Otherwise it returns (selectedText, true) and leaves the finalized
// highlight in place until the next event clears it.
func (m *Model) EndSelection() (string, bool) {
	if !m.sel.active {
		return "", false
	}
	m.sel.active = false
	if m.sel.anchor == m.sel.head {
		m.sel.has = false
		return "", false
	}
	m.sel.has = true
	return m.selectedText(), true
}

// ClearSelection drops all selection state.
func (m *Model) ClearSelection() {
	m.sel = selection{}
}

// HasSelection reports whether a finalized non-empty selection exists.
func (m *Model) HasSelection() bool {
	return m.sel.has
}

// IsSelecting reports whether a drag is currently in progress.
func (m *Model) IsSelecting() bool {
	return m.sel.active
}

// GutterCol returns the detected side-by-side divider column, or -1 for
// unified mode / undetected.
func (m *Model) GutterCol() int {
	return m.gutterCol
}

// DebugSelection exposes the current selection's anchor/head/band for tests.
// Only intended for diagnostics — not part of any public contract.
func (m *Model) DebugSelection() (anchor, head Point, band [2]int, active bool) {
	return m.sel.anchor, m.sel.head, m.sel.colBand, m.sel.active
}

// selectedText extracts the ANSI-stripped plaintext under the current
// selection from the cached diff content. Returns "" when no content is
// loaded or the relevant cached node has no diff text.
//
// A defer/recover guard turns any unexpected panic in the ANSI-cut path
// into an empty result rather than crashing the TUI on clipboard copy.
func (m *Model) selectedText() (result string) {
	defer func() {
		if r := recover(); r != nil {
			log.Warn("diffviewer: selectedText panicked; copying empty string", "panic", r)
			result = ""
		}
	}()
	return m.selectedTextInner()
}

func (m *Model) selectedTextInner() string {
	var src string
	switch {
	case m.file != nil:
		src = m.file.diff
	case m.dir != nil:
		src = m.dir.diff
	default:
		return ""
	}
	if src == "" {
		return ""
	}
	start, end := m.sel.normalized()
	lines := strings.Split(src, "\n")
	if start.Line < 0 || start.Line >= len(lines) {
		return ""
	}
	endLine := end.Line
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	out := make([]string, 0, endLine-start.Line+1)
	for i := start.Line; i <= endLine; i++ {
		line := lines[i]
		w := lipgloss.Width(line)
		a, b := highlightRange(i, start, end, w)
		a, b = clampToBand(a, b, m.sel.colBand)
		a, b = clampToLine(a, b, w)
		if a >= b {
			out = append(out, "")
			continue
		}
		out = append(out, ansi.Strip(ansi.Cut(line, a, b)))
	}
	return joinWrappedLines(out)
}

func diffFile(
	node *cachedNode,
	width int,
	sideBySide bool,
	themeArgs []string,
	renderID uint64,
) tea.Cmd {
	if width == 0 || node == nil || len(node.files) != 1 {
		return nil
	}

	file := node.files[0]
	key := cacheKey(node.path, sideBySide)
	return func() tea.Msg {
		// Only use side-by-side if preference is true AND file is not new/deleted
		useSideBySide := sideBySide && !file.IsNew && !file.IsDelete
		args := []string{
			"--paging=never",
			fmt.Sprintf("-w=%d", width),
		}
		// In side-by-side delta wraps on its own and the --max-line-length cap
		// keeps it from trying to render pathologically long lines. In unified
		// mode delta won't wrap — disable truncation entirely so the full line
		// reaches wrapLongLines.
		if useSideBySide {
			args = append(args, fmt.Sprintf("--max-line-length=%d", width))
		} else {
			args = append(args, "--max-line-length=0")
		}
		args = append(args, themeArgs...)
		if useSideBySide {
			args = append(args, "--side-by-side")
		}
		deltac := exec.Command("delta", args...)
		deltac.Env = os.Environ()
		deltac.Stdin = strings.NewReader(file.String() + "\n")
		out, err := deltac.Output()
		if err != nil {
			return common.ErrMsg{Err: err}
		}
		return diffContentMsg{cacheKey: key, text: string(out), renderID: renderID}
	}
}

func diffDir(
	dir *cachedNode,
	width int,
	sideBySide bool,
	themeArgs []string,
	selectedBg color.Color,
	preamble string,
	renderID uint64,
) tea.Cmd {
	if width == 0 || dir == nil {
		return nil
	}
	key := cacheKey(dir.path, sideBySide)
	return func() tea.Msg {
		s := lipgloss.NewStyle().Background(selectedBg)
		c := common.LipglossColorToHex(selectedBg)
		useSideBySide := sideBySide
		args := []string{
			"--paging=never",
			fmt.Sprintf("--file-modified-label=%s",
				utils.RemoveReset(s.Foreground(lipgloss.Yellow).Render(" "))),
			fmt.Sprintf("--file-removed-label=%s",
				utils.RemoveReset(s.Foreground(lipgloss.Red).Render(" "))),
			fmt.Sprintf("--file-added-label=%s",
				utils.RemoveReset(s.Foreground(lipgloss.Green).Render(" "))),
			fmt.Sprintf("--file-style='%s bold %s'", c, c),
			fmt.Sprintf("--file-decoration-style='%s box %s'", c, c),
			fmt.Sprintf("-w=%d", width),
		}
		// See diffFile: unified mode needs --max-line-length=0 so long lines
		// survive long enough for wrapLongLines to wrap them.
		if useSideBySide {
			args = append(args, fmt.Sprintf("--max-line-length=%d", width))
		} else {
			args = append(args, "--max-line-length=0")
		}
		args = append(args, themeArgs...)
		if useSideBySide {
			args = append(args, "--side-by-side")
		}
		deltac := exec.Command("delta", args...)
		deltac.Env = os.Environ()
		strs := strings.Builder{}
		for _, file := range dir.files {
			strs.WriteString(file.String())
		}
		deltac.Stdin = strings.NewReader(strs.String() + "\n")
		out, err := deltac.Output()
		if err != nil {
			return common.ErrMsg{Err: err}
		}

		text := string(out)
		if preamble != "" {
			text = renderPreamble(preamble) + "\n" + text
		}
		return diffContentMsg{cacheKey: key, text: text, renderID: renderID}
	}
}

func (m *Model) SetDarkBackground(isDark bool) tea.Cmd {
	if m.themeMode != themeAuto {
		return nil
	}
	if m.isDarkBackground != nil && *m.isDarkBackground == isDark {
		return nil
	}
	m.isDarkBackground = &isDark
	m.cache = make(nodeCache)
	return m.diff()
}

func (m Model) deltaThemeArgs() []string {
	if m.isDarkBackground == nil || *m.isDarkBackground {
		return nil
	}

	// Let delta drive the light-mode colors. Its --light defaults use named
	// ANSI colors that terminals render natively, which avoids the grey
	// downsampling we'd hit if we sent desaturated 24-bit hex like #ffebe9
	// through a piped subprocess.
	return []string{
		"--light",
		"--syntax-theme=GitHub",
	}
}

func parseThemeMode(v string) themeMode {
	switch config.NormalizeTheme(v) {
	case config.ThemeAuto:
		return themeAuto
	case config.ThemeLight:
		return themeLight
	case config.ThemeDark:
		return themeDark
	default:
		return themeAuto
	}
}

func renderPreamble(preamble string) string {
	preamble = strings.TrimSpace(preamble)
	if preamble == "" {
		return ""
	}

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	yellow := lipgloss.NewStyle().Foreground(lipgloss.Yellow)

	var out []string
	for _, line := range strings.Split(preamble, "\n") {
		switch {
		case strings.HasPrefix(line, "commit "):
			out = append(
				out,
				dim.Render("commit ")+yellow.Render(strings.TrimPrefix(line, "commit ")),
			)
		case strings.HasPrefix(line, "Author:"),
			strings.HasPrefix(line, "AuthorDate:"),
			strings.HasPrefix(line, "Date:"),
			strings.HasPrefix(line, "Commit:"),
			strings.HasPrefix(line, "CommitDate:"),
			strings.HasPrefix(line, "Merge:"):
			out = append(out, dim.Render(line))
		default:
			out = append(out, line)
		}
	}

	return strings.Join(out, "\n")
}

type diffContentMsg struct {
	cacheKey string
	text     string
	renderID uint64
}

func (m *Model) ClearCache() {
	m.cache = make(nodeCache)
}

func (m *Model) RootDiffStats() (int64, int64) {
	if item, ok := m.cache[cacheKey("/", m.sideBySide)]; ok {
		return item.additions, item.deletions
	}

	return 0, 0
}

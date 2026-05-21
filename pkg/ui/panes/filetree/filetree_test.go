package filetree

import (
	"os"
	"testing"

	"charm.land/bubbles/v2/tree"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
	"github.com/dlvhdr/diffnav/pkg/config"
	"github.com/dlvhdr/diffnav/pkg/constants"
	"github.com/dlvhdr/diffnav/pkg/dirnode"
	"github.com/dlvhdr/diffnav/pkg/filenode"
)

func TestClickDirectoryRowSelectsOnly(t *testing.T) {
	m := newTestTreeModel([]string{
		"app/main.go",
		"app/internal/db.go",
		"docs/readme.md",
	})
	app := nodeByPath(t, &m, "app")
	internal := nodeByPath(t, &m, "app/internal")
	docs := nodeByPath(t, &m, "docs")
	internal.Close()
	app.Close()
	m.SetCursorNoScroll(docs.YOffset())

	m.ClickNode(app)

	if got := m.CurrNodePath(); got != "app" {
		t.Fatalf("expected clicked directory to be selected, got %q", got)
	}
	if app.IsOpen() {
		t.Fatal("expected folded directory row click to leave it folded")
	}
	if internal.IsOpen() {
		t.Fatal("expected folded child directory to remain folded")
	}
}

func TestClickDirectoryIconSelectsAndTogglesOpenState(t *testing.T) {
	m := newTestTreeModel([]string{
		"app/main.go",
		"app/internal/db.go",
		"docs/readme.md",
	})
	app := nodeByPath(t, &m, "app")
	m.SetCursorNoScroll(app.YOffset())

	if !app.IsOpen() {
		t.Fatal("setup: expected app directory to start open")
	}
	m.ClickNodeIcon(app)
	if app.IsOpen() {
		t.Fatal("expected selected open directory icon click to close it")
	}

	m.ClickNodeIcon(app)
	if !app.IsOpen() {
		t.Fatal("expected selected folded directory icon click to open it")
	}
}

func TestClickDirectoryIconSelectsAndTogglesUnselectedDirectory(t *testing.T) {
	m := newTestTreeModel([]string{
		"app/main.go",
		"docs/readme.md",
	})
	app := nodeByPath(t, &m, "app")
	docs := nodeByPath(t, &m, "docs")
	m.SetCursorNoScroll(docs.YOffset())

	m.ClickNodeIcon(app)

	if got := m.CurrNodePath(); got != "app" {
		t.Fatalf("expected clicked directory to be selected, got %q", got)
	}
	if app.IsOpen() {
		t.Fatal("expected unselected open directory icon click to close it")
	}
}

func TestDirectoryIconHitUsesTreeRelativeColumn(t *testing.T) {
	m := newTestTreeModel([]string{
		"app/main.go",
		"app/internal/db.go",
		"docs/readme.md",
	})
	root := m.GetNodeAtY(0)
	app := nodeByPath(t, &m, "app")
	internal := nodeByPath(t, &m, "app/internal")

	for _, tc := range []struct {
		name string
		node *tree.Node
		x    int
	}{
		{name: "root icon", node: root, x: 0},
		{name: "depth one icon", node: app, x: 1},
		{name: "depth two icon", node: internal, x: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !m.IsDirectoryIconHit(tc.node, tc.x) {
				t.Fatalf("expected x=%d to hit %s", tc.x, tc.name)
			}
			if m.IsDirectoryIconHit(tc.node, tc.x-1) {
				t.Fatalf("expected x=%d to miss before %s", tc.x-1, tc.name)
			}
			if m.IsDirectoryIconHit(tc.node, tc.x+1) {
				t.Fatalf("expected x=%d to miss after %s icon", tc.x+1, tc.name)
			}
		})
	}
}

func TestDirectoryIconHitCoversNerdFontIconWidth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.UI.Icons = filenode.IconsNerdStatus
	m := New(cfg)
	files := []*gitdiff.File{
		{NewName: "app/main.go"},
		{NewName: "docs/readme.md"},
	}
	m = m.SetFiles(files)

	app := nodeByPath(t, &m, "app")
	start := directoryIconStartColumn(app)

	for _, x := range []int{start, start + 1} {
		if !m.IsDirectoryIconHit(app, x) {
			t.Fatalf("expected x=%d to hit full Nerd Font directory indicator", x)
		}
	}
	if m.IsDirectoryIconHit(app, start+2) {
		t.Fatalf("expected x=%d to miss after Nerd Font directory indicator", start+2)
	}
}

func TestDirectoryIconHitIgnoresFileNodes(t *testing.T) {
	m := newTestTreeModel([]string{"app/main.go"})
	file := nodeByPath(t, &m, "app/main.go")

	if m.IsDirectoryIconHit(file, file.Depth()) {
		t.Fatal("expected file node icon column to not be a directory toggle hit")
	}
}

func TestNodeDescendantDiffsIncludesFoldedChildren(t *testing.T) {
	m := newTestTreeModel([]string{
		"app/main.go",
		"app/internal/db.go",
		"docs/readme.md",
	})
	app := nodeByPath(t, &m, "app")
	internal := nodeByPath(t, &m, "app/internal")
	internal.Close()

	files := m.NodeDescendantDiffs(app)

	if len(files) != 2 {
		t.Fatalf("expected app diff to include folded child files, got %d", len(files))
	}
	got := []string{filenode.GetFileName(files[0]), filenode.GetFileName(files[1])}
	want := []string{"app/main.go", "app/internal/db.go"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected file %d to be %q, got %q", i, want[i], got[i])
		}
	}
}

func newTestTreeModel(paths []string) Model {
	cfg := config.DefaultConfig()
	cfg.UI.Icons = filenode.IconsASCII
	m := New(cfg)
	files := make([]*gitdiff.File, 0, len(paths))
	for _, p := range paths {
		files = append(files, &gitdiff.File{NewName: p})
	}
	return m.SetFiles(files)
}

func nodeByPath(t *testing.T, m *Model, path string) *tree.Node {
	t.Helper()
	for _, node := range m.t.AllNodes() {
		switch val := node.GivenValue().(type) {
		case *dirnode.DirNode:
			if val.FullPath == path {
				return node
			}
		case *filenode.FileNode:
			if filenode.GetFileName(val.File) == path {
				return node
			}
		}
	}
	t.Fatalf("expected to find node %q", path)
	return nil
}

// .
// ├── graphql-server
// │   └── tests
// │       └── package.json
// ├── yarn.lock
func TestBuildFullFileTree(t *testing.T) {
	f, err := os.Open("testdata/multiple_files.diff")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	tr := buildFullFileTree(files, config.Config{})
	allNodes := tr.AllNodes()
	if len(allNodes) != 5 {
		t.Fatalf("expected 5 nodes, but got %d", len(allNodes))
	}
	root := tr
	if root.GivenValue().(*dirnode.DirNode).Name != constants.RootName {
		t.Fatalf(`expected root value to be constants.RootName, but got "%s"`, root.Value())
	}

	if len(root.ChildNodes()) != 2 {
		t.Fatalf("expected root to have 2 children, but got %d", len(root.ChildNodes()))
	}

	graphqlServer := root.ChildNodes()[0]
	if graphqlServer.GivenValue().(*dirnode.DirNode).Name != "graphql-server" {
		t.Fatalf(
			`expected root first child value to be "graphql-server", but got %s`,
			graphqlServer.GivenValue(),
		)
	}
	yarnLock := root.ChildNodes()[1]
	if yarnLock.GivenValue().(*filenode.FileNode).Path() != "yarn.lock" {
		t.Log(tr.String())
		t.Fatalf(`expected root second child value to be "* yarn.lock", but got %s`,
			yarnLock.GivenValue().(*filenode.FileNode).Path())
	}

	if len(graphqlServer.ChildNodes()) != 1 {
		t.Fatalf(
			"expected graphql-server to have 1 children, but got %d",
			len(graphqlServer.ChildNodes()),
		)
	}

	tests := graphqlServer.ChildNodes()[0]
	if tests.GivenValue().(*dirnode.DirNode).Name != "tests" {
		t.Fatalf(
			`expected graphql-server only child value to be "tests", but got %s`,
			tests.GivenValue(),
		)
	}

	if len(tests.ChildNodes()) != 1 {
		t.Fatalf("expected tests to have 1 children, but got %d", len(tests.ChildNodes()))
	}

	packageJson := tests.ChildNodes()[0]
	if packageJson.GivenValue().(*filenode.FileNode).Path() != "graphql-server/tests/package.json" {
		t.Fatalf(
			`expected tests only child value to be "graphql-server/tests/package.json", but got %s`,
			packageJson.GivenValue().(*filenode.FileNode).Path(),
		)
	}
}

// input:
// .
// ├── graphql-server
// │   └── tests
// │       └── package.json
// └── yarn.lock
//
// output:
// .
// ├── graphql-server/tests
// │   └── package.json
// └── yarn.lock
func TestCollapseTree(t *testing.T) {
	f, err := os.Open("testdata/multiple_files.diff")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	tr := buildFullFileTree(files, config.Config{})
	tr = collapseTree(tr)

	allNodes := tr.AllNodes()
	if len(allNodes) != 4 {
		t.Fatalf("expected 4 nodes, but got %d", len(allNodes))
	}

	root := tr
	if root.GivenValue().(*dirnode.DirNode).Name != constants.RootName {
		t.Fatalf(`expected root value to be constants.RootName, but got "%s"`, root.Value())
	}

	if len(root.ChildNodes()) != 2 {
		t.Fatalf("expected root to have 2 children, but got %d", len(root.ChildNodes()))
	}

	graphqlServer := root.ChildNodes()[0]
	if graphqlServer.GivenValue().(*dirnode.DirNode).Name != "graphql-server/tests" {
		t.Fatalf(
			`expected root first child value to be "graphql-server/tests", but got %s`,
			graphqlServer.GivenValue(),
		)
	}

	if len(graphqlServer.ChildNodes()) != 1 {
		t.Fatalf(
			"expected graphql-server to have 1 children, but got %d",
			len(graphqlServer.ChildNodes()),
		)
	}
	packageJson := graphqlServer.ChildNodes()[0]
	if packageJson.GivenValue().(*filenode.FileNode).Path() != "graphql-server/tests/package.json" {
		t.Fatalf(
			`expected graphql-server/tests only child value to be "graphql-server/tests/package.json", but got %s`,
			packageJson.GivenValue(),
		)
	}

	yarnLock := root.ChildNodes()[1]
	if yarnLock.GivenValue().(*filenode.FileNode).Path() != "yarn.lock" {
		t.Log(tr.String())
		t.Fatalf(`expected root second child value to be "* yarn.lock", but got %s`,
			yarnLock.GivenValue().(*filenode.FileNode).Path())
	}
}

// input:
// .
// └── ui
//     ├── components
//     │   ├── reposection
//     │   │   ├── commands.go
//     │   │   └── reposection.go
//     │   ├── section
//     │   │   └── section.go
//     │   └── tasks
//     │       └── pr.go
//     └─ keys
//     │   └── branchkeys.go
//     └── ui.go

// output is the same as there are no collapsible nodes
func TestUncollapsableTree(t *testing.T) {
	f, err := os.Open("testdata/gh_dash_pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	tr := buildFullFileTree(files, config.Config{})

	tr = collapseTree(tr)
	allNodes := tr.AllNodes()
	if len(allNodes) != 13 {
		t.Fatalf("expected 13 nodes, but got %d", len(allNodes))
	}
}

func TestCloseDirsBelowDepthZero(t *testing.T) {
	f, err := os.Open("testdata/multiple_files.diff")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	tr := buildFullFileTree(files, config.Config{})
	tr = collapseTree(tr)

	treeModel := tree.New(nil, 80, 40)
	treeModel.SetNodes(tr)

	root := treeModel.Root()

	allNodesBefore := root.AllNodes()
	if len(allNodesBefore) != 4 {
		t.Fatalf("expected 4 nodes before closing, but got %d", len(allNodesBefore))
	}

	closeDirsBelow(root, 0)

	if !root.IsOpen() {
		t.Fatal("expected root node to remain open")
	}

	allNodesAfter := root.AllNodes()
	if len(allNodesAfter) >= len(allNodesBefore) {
		t.Fatalf("expected fewer visible nodes after closing dirs, got %d (before: %d)",
			len(allNodesAfter), len(allNodesBefore))
	}

	for _, node := range allNodesAfter {
		if _, ok := node.GivenValue().(*dirnode.DirNode); ok {
			if node.Depth() > 0 && node.IsOpen() {
				t.Fatalf("expected directory at depth %d to be closed", node.Depth())
			}
		}
	}
}

func TestCloseDirsBelowDepthOne(t *testing.T) {
	f, err := os.Open("testdata/gh_dash_pr.diff")
	if err != nil {
		t.Fatal(err)
	}
	files, _, err := gitdiff.Parse(f)
	if err != nil {
		t.Fatal(err)
	}

	tr := buildFullFileTree(files, config.Config{})
	tr = collapseTree(tr)

	treeModel := tree.New(nil, 80, 40)
	treeModel.SetNodes(tr)

	root := treeModel.Root()

	closeDirsBelow(root, 1)

	if !root.IsOpen() {
		t.Fatal("expected root node to remain open")
	}

	for _, node := range root.ChildNodes() {
		if dir, ok := node.GivenValue().(*dirnode.DirNode); ok {
			if !node.IsOpen() {
				t.Fatalf("expected depth-1 directory %q to be open", dir.Name)
			}
			for _, child := range node.ChildNodes() {
				if _, ok := child.GivenValue().(*dirnode.DirNode); ok {
					if child.IsOpen() {
						t.Fatalf("expected depth-2 directory to be closed")
					}
				}
			}
		}
	}
}

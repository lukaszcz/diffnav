package diffviewer

import "testing"

func TestUpdateIgnoresStaleDiffContentMsg(t *testing.T) {
	m := New(false, "auto")
	m.vp.SetWidth(120)
	key := cacheKey("/", false)
	m.cache[key] = &cachedNode{}
	m.renderID = 2

	updated, _ := m.Update(diffContentMsg{
		cacheKey:  key,
		text:      "stale",
		renderID:  1,
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
		cacheKey:  key,
		text:      "fresh",
		renderID:  3,
	})

	if updated.cache[key].diff != "fresh" {
		t.Fatalf("expected current render to be applied, got %q", updated.cache[key].diff)
	}
}

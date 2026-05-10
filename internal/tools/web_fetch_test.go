package tools

import "testing"

func TestRemoveElementsHandlesUnicodeCaseFolding(t *testing.T) {
	html := "<script>\u0130</script><p>ok</p>"

	got := removeElements(html, []string{"script"})
	if got != "<p>ok</p>" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRemoveClassElementsHandlesUnicodeCaseFolding(t *testing.T) {
	html := "<div class=\"sidebar\">\u0130</div><p>ok</p>"

	got := removeClassElements(html, []string{"sidebar"})
	if got != "<p>ok</p>" {
		t.Fatalf("unexpected output: %q", got)
	}
}

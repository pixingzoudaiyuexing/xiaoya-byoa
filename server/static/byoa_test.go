package static

import (
	"strings"
	"testing"
)

func TestBYOAVisitorScriptInjection(t *testing.T) {
	input := "<html><body><main>catalog</main></body></html>"
	got := injectBYOAVisitorScript(input)

	if strings.Count(got, `data-xiaoya-byoa="mvp"`) != 1 {
		t.Fatalf("BYOA script marker count = %d, want 1", strings.Count(got, `data-xiaoya-byoa="mvp"`))
	}
	if !strings.Contains(got, "XMLHttpRequest.prototype.open") {
		t.Fatal("BYOA visitor script missing XHR interception")
	}
	if !strings.Contains(got, "window.fetch = function") {
		t.Fatal("BYOA visitor script missing fetch interception")
	}
	if !strings.Contains(got, `/api/fs/get`) {
		t.Fatal("BYOA visitor script missing fs/get guard")
	}
	if strings.Index(got, `data-xiaoya-byoa="mvp"`) > strings.Index(got, "</body>") {
		t.Fatal("BYOA visitor script should be injected before </body>")
	}
}

func TestBYOAVisitorScriptDoesNotDuplicate(t *testing.T) {
	first := injectBYOAVisitorScript("<html><body></body></html>")
	second := injectBYOAVisitorScript(first)
	if first != second {
		t.Fatal("second BYOA injection changed already-injected HTML")
	}
}

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
	if !strings.Contains(got, `method: "POST"`) || !strings.Contains(got, `JSON.stringify(data || {})`) {
		t.Fatal("BYOA visitor script must POST QR status parameters as JSON")
	}
	if strings.Contains(got, `/status?`) || strings.Contains(got, `encodeURIComponent(session.ck`) || strings.Contains(got, `encodeURIComponent(session.token`) {
		t.Fatal("BYOA visitor script must not place QR status secrets in URL query strings")
	}
	if !strings.Contains(got, `path.slice(-7) !== "/@login"`) || !strings.Contains(got, `parsed.origin !== location.origin`) {
		t.Fatal("BYOA visitor script must validate login redirects as same-origin paths")
	}
	if !strings.Contains(got, `location.replace(target)`) || !strings.Contains(got, `setTimeout(finishAuthorization, 700)`) {
		t.Fatal("BYOA visitor script must return to the original media after authorization")
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

func TestBYOAVisitorScriptHidesREADMEFetchErrors(t *testing.T) {
	got := injectBYOAVisitorScript("<html><body></body></html>")
	for _, marker := range []string{"hideREADMEFetchError", "README\\\\.md", "Failed to fetch", "MutationObserver"} {
		if !strings.Contains(got, marker) {
			t.Fatalf("injected script missing README error guard marker %q", marker)
		}
	}
}

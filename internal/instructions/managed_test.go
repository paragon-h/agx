package instructions

import (
	"bytes"
	"testing"
)

func TestRenderReplaceAndRemovePreservesUserContent(t *testing.T) {
	original := []byte("# Personal guidance\n")
	first, err := Render(original, []byte("Use tests.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(first, original) || !bytes.Contains(first, []byte("Use tests.")) {
		t.Fatalf("rendered content = %q", first)
	}
	second, err := Render(first, []byte("Run tests.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(second, []byte("Use tests.")) || !bytes.Contains(second, []byte("Run tests.")) {
		t.Fatalf("updated content = %q", second)
	}
	removed, found, err := Remove(second)
	if err != nil || !found {
		t.Fatalf("Remove() found = %v, err = %v", found, err)
	}
	if !bytes.Contains(removed, []byte("# Personal guidance")) || bytes.Contains(removed, []byte(BeginMarker)) {
		t.Fatalf("removed content = %q", removed)
	}
}

func TestParseRejectsMalformedAndDuplicateMarkers(t *testing.T) {
	values := [][]byte{
		[]byte(BeginMarker + "\nmissing end\n"),
		[]byte(BeginMarker + "\na\n" + EndMarker + "\n" + BeginMarker + "\nb\n" + EndMarker + "\n"),
		[]byte("prefix " + BeginMarker + "\na\n" + EndMarker + "\n"),
	}
	for _, value := range values {
		if _, err := Parse(value); err == nil {
			t.Fatalf("Parse(%q) error = nil", value)
		}
	}
}

func TestRenderUsesCRLFForManagedMarkers(t *testing.T) {
	rendered, err := Render([]byte("user\r\n"), []byte("managed\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rendered, []byte("\n")) && !bytes.Contains(rendered, []byte("\r\n")) {
		t.Fatalf("rendered content does not preserve CRLF: %q", rendered)
	}
}

package util

import (
	"strings"
	"testing"
)

func TestMarshalJSONNoHTMLEscape(t *testing.T) {
	v := map[string]any{"s": "a<b>&c", "n": 1.0}
	b, err := MarshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "a<b>&c") {
		t.Errorf("HTML chars escaped: %s", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("trailing newline: %q", got)
	}
}

func TestMarshalJSONIndent(t *testing.T) {
	v := map[string]any{"a": 1, "b": map[string]any{"c": "x<y>"}}
	b, err := MarshalJSONIndent(v)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := "{\n  \"a\": 1,\n  \"b\": {\n    \"c\": \"x<y>\"\n  }\n}"
	if got != want {
		t.Errorf("indent mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestMarshalFloatWholeNumbers(t *testing.T) {
	// userInfo byte counts must serialize as integers, not 100.0.
	v := map[string]any{"upload": float64(100), "expire": float64(1800000000)}
	b, err := MarshalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "\"upload\":100") || !strings.Contains(got, "\"expire\":1800000000") {
		t.Errorf("whole floats not integer-formatted: %s", got)
	}
}

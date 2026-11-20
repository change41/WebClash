package util

import "testing"

func TestEncodeURIComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"节点A", "%E8%8A%82%E7%82%B9A"},
		{"a b", "a%20b"},
		{"a+b", "a%2Bb"},
		{"~!*'()", "~!*'()"},
		{"-_.", "-_."},
		{"abcXYZ019", "abcXYZ019"},
		{"/?=&#", "%2F%3F%3D%26%23"},
		{"100%", "100%25"},
		{"café", "caf%C3%A9"},
		{" PROXY ", "%20PROXY%20"},
	}
	for _, c := range cases {
		if got := EncodeURIComponent(c.in); got != c.want {
			t.Errorf("EncodeURIComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

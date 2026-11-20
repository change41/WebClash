package util

import "testing"

func f64(v float64) *float64 { return &v }

func TestParseUserInfo(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   *SubscriptionUserInfo
	}{
		{"full", "upload=100; download=200; total=300; expire=1800000000",
			&SubscriptionUserInfo{Upload: f64(100), Download: f64(200), Total: f64(300), Expire: f64(1800000000)}},
		{"partial", "upload=100", &SubscriptionUserInfo{Upload: f64(100)}},
		{"no spaces", "upload=100;download=200", &SubscriptionUserInfo{Upload: f64(100), Download: f64(200)}},
		{"trim", " upload = 100 ; total = 300 ", &SubscriptionUserInfo{Upload: f64(100), Total: f64(300)}},
		{"empty value is zero", "upload=", &SubscriptionUserInfo{Upload: f64(0)}},
		{"extra equals", "upload=1=2", &SubscriptionUserInfo{Upload: f64(1)}},
		{"empty header", "", nil},
		{"unknown key", "foo=bar", nil},
		{"non numeric", "upload=abc", nil},
		{"infinity", "upload=Infinity", nil},
		{"no equals", "upload", nil},
		{"mixed valid invalid", "upload=100; download=abc; total=300",
			&SubscriptionUserInfo{Upload: f64(100), Total: f64(300)}},
	}
	for _, c := range cases {
		got := ParseUserInfo(c.header)
		if !userInfoEqual(got, c.want) {
			t.Errorf("%s: ParseUserInfo(%q) = %+v, want %+v", c.name, c.header, deref(got), deref(c.want))
		}
	}
}

func userInfoEqual(a, b *SubscriptionUserInfo) bool {
	if a == nil || b == nil {
		return a == b
	}
	return ptrEq(a.Upload, b.Upload) && ptrEq(a.Download, b.Download) &&
		ptrEq(a.Total, b.Total) && ptrEq(a.Expire, b.Expire)
}

func ptrEq(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(u *SubscriptionUserInfo) any {
	if u == nil {
		return nil
	}
	return SubscriptionUserInfo{Upload: u.Upload, Download: u.Download, Total: u.Total, Expire: u.Expire}
}

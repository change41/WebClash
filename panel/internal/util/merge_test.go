package util

import (
	"reflect"
	"testing"
)

func TestDeepMerge(t *testing.T) {
	cases := []struct {
		name        string
		base, patch any
		want        any
	}{
		{
			"nested objects",
			map[string]any{"a": map[string]any{"x": 1, "y": 2}, "b": 3},
			map[string]any{"a": map[string]any{"y": 9, "z": 4}},
			map[string]any{"a": map[string]any{"x": 1, "y": 9, "z": 4}, "b": 3},
		},
		{
			"array replaces wholesale",
			map[string]any{"list": []any{1, 2, 3}},
			map[string]any{"list": []any{9}},
			map[string]any{"list": []any{9}},
		},
		{
			"null is a value",
			map[string]any{"a": 1},
			map[string]any{"a": nil},
			map[string]any{"a": nil},
		},
		{
			"scalar replaces object",
			map[string]any{"a": map[string]any{"x": 1}},
			map[string]any{"a": 5},
			map[string]any{"a": 5},
		},
		{
			"object replaces scalar",
			map[string]any{"a": 5},
			map[string]any{"a": map[string]any{"x": 1}},
			map[string]any{"a": map[string]any{"x": 1}},
		},
		{"base not object", 5, map[string]any{"a": 1}, map[string]any{"a": 1}},
		{"patch not object", map[string]any{"a": 1}, 5, 5},
		{"patch null", map[string]any{"a": 1}, nil, nil},
	}
	for _, c := range cases {
		got := DeepMerge(c.base, c.patch)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: DeepMerge = %#v, want %#v", c.name, got, c.want)
		}
	}
}

func TestIsPlainObject(t *testing.T) {
	if !IsPlainObject(map[string]any{}) {
		t.Error("map should be plain object")
	}
	for _, v := range []any{nil, 5, "s", []any{}, true} {
		if IsPlainObject(v) {
			t.Errorf("%#v should not be plain object", v)
		}
	}
}

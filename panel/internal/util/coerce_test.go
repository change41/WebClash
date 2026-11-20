package util

import (
	"math"
	"testing"
)

func TestJsNumber(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
	}{
		{"undefined", UndefinedValue, math.NaN()},
		{"null", nil, 0},
		{"true", true, 1},
		{"false", false, 0},
		{"float", 12.5, 12.5},
		{"int", 7, 7},
		{"int64", int64(9), 9},
		{"numstring", "123", 123},
		{"padded", "  12.5  ", 12.5},
		{"empty", "", 0},
		{"spaces", "   ", 0},
		{"infinity", "Infinity", math.Inf(1)},
		{"plusinfinity", "+Infinity", math.Inf(1)},
		{"minusinfinity", "-Infinity", math.Inf(-1)},
		{"exponent", "1e3", 1000},
		{"negative", "-42", -42},
		{"garbage", "abc", math.NaN()},
		{"trailing", "12abc", math.NaN()},
		{"array", []any{}, math.NaN()},
		{"object", map[string]any{}, math.NaN()},
	}
	for _, c := range cases {
		got := JsNumber(c.in)
		if math.IsNaN(c.want) {
			if !math.IsNaN(got) {
				t.Errorf("%s: JsNumber(%v) = %v, want NaN", c.name, c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: JsNumber(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

func TestJsRound(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0.5, 1}, {1.5, 2}, {2.5, 3}, {1.4, 1}, {1.6, 2},
		{-0.5, 0}, {-1.5, -1}, {-1.6, -2}, {0, 0}, {3, 3},
		{math.NaN(), math.NaN()}, {math.Inf(1), math.Inf(1)}, {math.Inf(-1), math.Inf(-1)},
	}
	for _, c := range cases {
		got := JsRound(c.in)
		if math.IsNaN(c.want) {
			if !math.IsNaN(got) {
				t.Errorf("JsRound(%v) = %v, want NaN", c.in, got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("JsRound(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestJsIsInteger(t *testing.T) {
	cases := []struct {
		in   float64
		want bool
	}{
		{1, true}, {1.0, true}, {0, true}, {-3, true}, {1.5, false},
		{math.NaN(), false}, {math.Inf(1), false}, {math.Inf(-1), false},
	}
	for _, c := range cases {
		if got := JsIsInteger(c.in); got != c.want {
			t.Errorf("JsIsInteger(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

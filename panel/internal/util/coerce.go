package util

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Undefined represents a JS `undefined` value (a missing object key), which is
// distinct from JSON null. Number(undefined) is NaN while Number(null) is 0.
type Undefined struct{}

// UndefinedValue is the singleton undefined sentinel.
var UndefinedValue = Undefined{}

// JsNumber replicates JS Number() coercion for JSON-derived values.
func JsNumber(v any) float64 {
	switch x := v.(type) {
	case Undefined:
		return math.NaN()
	case nil:
		return 0 // Number(null) === 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		return jsNumberString(x)
	default:
		return math.NaN()
	}
}

func jsNumberString(s string) float64 {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0 // Number("") === 0
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f
	}
	return math.NaN()
}

// JsRound replicates JS Math.round (floor(x + 0.5)), preserving NaN/Inf.
func JsRound(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	return math.Floor(x + 0.5)
}

// JsIsInteger reports whether x is a finite whole number (Number.isInteger).
func JsIsInteger(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0) && x == math.Trunc(x)
}

// JsStringLength counts UTF-16 code units, matching JS String.prototype.length
// (astral characters count as 2). Used for the password minimum-length check.
func JsStringLength(s string) int {
	return len(utf16.Encode([]rune(s)))
}

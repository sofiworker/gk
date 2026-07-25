package ghttp

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

type publicParamValue struct{ value any }

func (v publicParamValue) MarshalPublicParam() (any, error) { return v.value, nil }

type failingPublicParam struct{ panic bool }

func (v failingPublicParam) MarshalPublicParam() (any, error) {
	if v.panic {
		panic("bad marshaler")
	}
	return nil, errors.New("bad marshaler")
}

func TestSanitizePublicParamsAllowsProtocolValues(t *testing.T) {
	now := time.Date(2026, 7, 25, 1, 2, 3, 0, time.UTC)
	got, diagnostic := sanitizePublicParams(map[string]any{
		"nil": nil, "bool": true, "string": "ok", "int": int64(42),
		"float": 1.5, "time": now, "duration": 1500 * time.Millisecond,
		"number": json.Number("12.5"), "slice": []string{"a", "b"},
		"map": map[string]int{"a": 1}, "custom": publicParamValue{value: map[string]any{"ok": true}},
	})
	if diagnostic {
		t.Fatal("valid params produced diagnostics")
	}
	if got["time"] != now.Format(time.RFC3339Nano) || got["duration"] != "1.5s" {
		t.Fatalf("time values = %#v, %#v", got["time"], got["duration"])
	}
	if _, ok := got["custom"].(map[string]any); !ok {
		t.Fatalf("custom = %#v", got["custom"])
	}
}

func TestSanitizePublicParamsDropsUnsafeValues(t *testing.T) {
	value := 42
	var typedNil *int
	cycle := map[string]any{}
	cycle["self"] = cycle
	got, diagnostic := sanitizePublicParams(map[string]any{
		"error": errors.New("secret"), "pointer": &value, "typed_nil": typedNil,
		"struct": struct{ Secret string }{"x"}, "nan": math.NaN(), "inf": math.Inf(1),
		"number": json.Number("nope"), "cycle": cycle,
		"marshal_error": failingPublicParam{}, "marshal_panic": failingPublicParam{panic: true},
	})
	if !diagnostic {
		t.Fatal("unsafe params should produce diagnostics")
	}
	if len(got) != 0 {
		t.Fatalf("unsafe params leaked: %#v", got)
	}
}

func TestSanitizePublicParamsAppliesLimits(t *testing.T) {
	params := make(map[string]any)
	for i := 39; i >= 0; i-- {
		params[string(rune('a'+i/10))+string(rune('0'+i%10))] = i
	}
	params["oversized"] = strings.Repeat("x", 4097)
	params["large_slice"] = make([]int, 65)
	params["too_deep"] = map[string]any{"1": map[string]any{"2": map[string]any{"3": map[string]any{"4": map[string]any{"5": map[string]any{"6": map[string]any{"7": map[string]any{"8": map[string]any{"9": true}}}}}}}}}

	got, diagnostic := sanitizePublicParams(params)
	if !diagnostic || len(got) != 32 {
		t.Fatalf("len=%d diagnostic=%v", len(got), diagnostic)
	}
	if _, ok := got["oversized"]; ok {
		t.Fatal("oversized string was retained")
	}
	keys := sortedMapKeys(got)
	if keys[0] != "a0" || keys[len(keys)-1] != "d1" {
		t.Fatalf("retained keys = %v", keys)
	}
}

func TestSanitizePublicParamsClearsOversizedJSON(t *testing.T) {
	params := make(map[string]any, 32)
	for i := 0; i < 32; i++ {
		params[string(rune('a'+i/10))+string(rune('0'+i%10))] = strings.Repeat("x", 2000)
	}
	got, diagnostic := sanitizePublicParams(params)
	if !diagnostic || len(got) != 0 {
		t.Fatalf("got len=%d diagnostic=%v", len(got), diagnostic)
	}
}

func TestSanitizePublicParamsReturnsDefensiveCopy(t *testing.T) {
	nested := map[string]any{"id": 42}
	got, _ := sanitizePublicParams(map[string]any{"nested": nested})
	nested["id"] = 7
	if got["nested"].(map[string]any)["id"] != int64(42) {
		t.Fatal("sanitized params share input map")
	}
}

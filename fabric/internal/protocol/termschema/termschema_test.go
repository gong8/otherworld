package termschema_test

import (
	"encoding/json"
	"testing"

	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/protocol/termschema"
)

const termsDir = "../../../../proto/terms"

// mustLoad is a test helper that calls Load and fatals on error.
func mustLoad(t *testing.T) *termschema.Registry {
	t.Helper()
	r, err := termschema.Load(termsDir)
	if err != nil {
		t.Fatalf("Load(%q): %v", termsDir, err)
	}
	return r
}

func terms(typ string, value any) protocol.Terms {
	b, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return protocol.Terms{Type: typ, Value: json.RawMessage(b)}
}

// temperature.set: 20.5 is within [5,30] → valid.
func TestTemperatureSetValidValue(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("temperature.set", 20.5)); err != nil {
		t.Fatalf("temperature.set 20.5 must be valid: %v", err)
	}
}

// temperature.set: 99 exceeds maximum 30 → invalid.
func TestTemperatureSetTooHigh(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("temperature.set", 99)); err == nil {
		t.Fatal("temperature.set 99 must be invalid (exceeds maximum 30)")
	}
}

// temperature.set: 4.0 is below minimum 5 → invalid.
func TestTemperatureSetTooLow(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("temperature.set", 4.0)); err == nil {
		t.Fatal("temperature.set 4.0 must be invalid (below minimum 5)")
	}
}

// lamp.set: "dim" is in the enum → valid.
func TestLampSetValidEnum(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("lamp.set", "dim")); err != nil {
		t.Fatalf("lamp.set \"dim\" must be valid: %v", err)
	}
}

// lamp.set: "blazing" is not in the enum → invalid.
func TestLampSetInvalidEnum(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("lamp.set", "blazing")); err == nil {
		t.Fatal("lamp.set \"blazing\" must be invalid (not in enum)")
	}
}

// trade with all five required fields → valid.
func TestTradeValid(t *testing.T) {
	r := mustLoad(t)
	v := map[string]any{
		"give":        "lamp",
		"get":         "marks",
		"price_marks": 5,
		"buyer":       "voice:buyer-agent",
		"seller":      "voice:seller-agent",
	}
	if err := r.Validate(terms("trade", v)); err != nil {
		t.Fatalf("trade with all five fields must be valid: %v", err)
	}
}

// trade with an extra field → invalid (additionalProperties false on value).
func TestTradeExtraFieldRejected(t *testing.T) {
	r := mustLoad(t)
	v := map[string]any{
		"give":        "lamp",
		"get":         "marks",
		"price_marks": 5,
		"buyer":       "voice:buyer-agent",
		"seller":      "voice:seller-agent",
		"extra":       "forbidden",
	}
	if err := r.Validate(terms("trade", v)); err == nil {
		t.Fatal("trade with extra field must be invalid (additionalProperties false)")
	}
}

// Unknown type → error (registry is closed).
func TestUnknownTypeRejected(t *testing.T) {
	r := mustLoad(t)
	if err := r.Validate(terms("door.unlock", "open")); err == nil {
		t.Fatal("unknown type \"door.unlock\" must be invalid (registry is closed)")
	}
}

// Load on a missing directory → error.
func TestLoadMissingDirErrors(t *testing.T) {
	if _, err := termschema.Load("/nonexistent/path/to/terms"); err == nil {
		t.Fatal("Load on missing directory must return an error")
	}
}

package world_test

import (
	"encoding/json"
	"testing"

	"otherworld/fabric/internal/protocol"
	"otherworld/fabric/internal/world"
)

func terms(typ, val string) protocol.Terms {
	return protocol.Terms{Type: typ, Value: json.RawMessage(val)}
}

func TestTemperatureReducer(t *testing.T) {
	w := world.New()
	w.Register("voice:heating", world.ThingState{"temperature": 21.0})
	if err := w.Apply("voice:heating", terms("temperature.set", `20.5`)); err != nil {
		t.Fatal(err)
	}
	if got := w.View("voice:heating")["temperature"]; got != 20.5 {
		t.Fatalf("temperature = %v, want 20.5", got)
	}
}

func TestLedgerTransfersMarks(t *testing.T) {
	w := world.New()
	w.Credit("voice:buyer", 100)
	w.Credit("voice:seller", 0)
	tr := terms("trade", `{"give":"one biscuit","get":"3 marks","price_marks":3,"buyer":"voice:buyer","seller":"voice:seller"}`)
	if err := w.Apply("voice:seller", tr); err != nil {
		t.Fatal(err)
	}
	if w.Marks("voice:buyer") != 97 || w.Marks("voice:seller") != 3 {
		t.Fatalf("marks: buyer=%d seller=%d, want 97/3", w.Marks("voice:buyer"), w.Marks("voice:seller"))
	}
}

func TestInsufficientMarksRejected(t *testing.T) {
	w := world.New()
	w.Credit("voice:buyer", 1)
	tr := terms("trade", `{"give":"x","get":"y","price_marks":3,"buyer":"voice:buyer","seller":"voice:s"}`)
	if err := w.Apply("voice:s", tr); err == nil {
		t.Fatal("expected insufficient-marks error")
	}
}

func TestUnknownTermTypeRejected(t *testing.T) {
	w := world.New()
	if err := w.Apply("voice:heating", terms("door.unlock", `true`)); err == nil {
		t.Fatal("unknown term type must be rejected (law 6: typed terms only)")
	}
}

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

func TestNegativePriceRejected(t *testing.T) {
	w := world.New()
	w.Credit("voice:buyer", 100)
	w.Credit("voice:seller", 0)
	tr := terms("trade", `{"give":"x","get":"y","price_marks":-5,"buyer":"voice:buyer","seller":"voice:seller"}`)
	if err := w.Apply("voice:seller", tr); err == nil {
		t.Fatal("negative price must be rejected (would mint marks)")
	}
	if w.Marks("voice:buyer") != 100 || w.Marks("voice:seller") != 0 {
		t.Fatalf("marks changed: buyer=%d seller=%d, want 100/0", w.Marks("voice:buyer"), w.Marks("voice:seller"))
	}
}

func TestSetOnUnregisteredVoiceRejected(t *testing.T) {
	w := world.New()
	if err := w.Apply("voice:ghost", terms("temperature.set", `19.5`)); err == nil {
		t.Fatal("set on unregistered thing must be rejected, not silently no-op")
	}
	if err := w.Apply("voice:ghost", terms("lamp.set", `"dim"`)); err == nil {
		t.Fatal("lamp.set on unregistered thing must be rejected")
	}
}

func TestLampReducer(t *testing.T) {
	w := world.New()
	w.Register("voice:lamp", world.ThingState{"lamp": "off"})
	if err := w.Apply("voice:lamp", terms("lamp.set", `"dim"`)); err != nil {
		t.Fatal(err)
	}
	if got := w.View("voice:lamp")["lamp"]; got != "dim" {
		t.Fatalf("lamp = %v, want dim", got)
	}
}

func TestCurtainsReducer(t *testing.T) {
	w := world.New()
	w.Register("voice:curtains", world.ThingState{"curtains": "open"})
	if err := w.Apply("voice:curtains", terms("curtains.set", `"closed"`)); err != nil {
		t.Fatal(err)
	}
	if got := w.View("voice:curtains")["curtains"]; got != "closed" {
		t.Fatalf("curtains = %v, want closed", got)
	}
}

func TestViewReturnsCopy(t *testing.T) {
	w := world.New()
	w.Register("voice:heating", world.ThingState{"temperature": 21.0})
	v := w.View("voice:heating")
	v["temperature"] = 99.0
	if got := w.View("voice:heating")["temperature"]; got != 21.0 {
		t.Fatalf("mutating a View leaked into world state: temperature = %v, want 21.0 (law 6)", got)
	}
}

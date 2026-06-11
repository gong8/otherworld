package protocol_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"otherworld/fabric/internal/protocol"
)

func compile(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnvelopeAgreesWithSchema(t *testing.T) {
	s := compile(t, "../../../proto/envelope.schema.json")
	terms := protocol.Terms{Type: "temperature.set", Value: json.RawMessage(`20.5`)}
	env := protocol.Envelope{
		V: 0, ID: "utt_01J0000000000000000000TEST", TS: time.Now().UTC(),
		From: "voice:heating", Serves: "the household", Scope: "scope:household",
		To: []string{"voice:her-agent"}, Kind: protocol.KindPropose,
		Exchange: "exc_01J0000000000000000000TEST",
		Body:     "i can hold the middle.", Terms: &terms,
	}
	b, _ := json.Marshal(env)
	var v any
	_ = json.Unmarshal(b, &v)
	if err := s.Validate(v); err != nil {
		t.Fatalf("Go Envelope does not satisfy proto schema: %v\n%s", err, b)
	}
}

func TestCharterAgreesWithSchema(t *testing.T) {
	s := compile(t, "../../../proto/charter.schema.json")
	ch := protocol.Charter{
		Voice: "voice:corner-shop", Serves: "the shopkeeper", Kind: protocol.VoiceThing,
		Interests: "sell small comforts at fair terms.",
		Mandate: protocol.Mandate{
			MayProposeTerms: []string{"trade"}, MaySettleWithoutPrincipal: false, SpendLimitMarks: 0,
		},
	}
	b, _ := json.Marshal(ch)
	var v any
	_ = json.Unmarshal(b, &v)
	if err := s.Validate(v); err != nil {
		t.Fatalf("Go Charter does not satisfy proto schema: %v\n%s", err, b)
	}
}

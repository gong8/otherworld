// Package termschema compiles the proto/terms registry and validates term
// payloads. The registry is closed: a term type without a schema is invalid
// (law 6 — only registered terms may touch the world).
package termschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"otherworld/fabric/internal/protocol"
)

// Registry holds compiled JSON schemas keyed by term type name.
type Registry struct {
	schemas map[string]*jsonschema.Schema
}

// Load compiles every *.json in dir. The type name is the filename minus .json
// (e.g. temperature.set.json → "temperature.set"). Returns an error if dir
// does not exist or any schema fails to compile.
func Load(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("termschema.Load: cannot read dir %q: %w", dir, err)
	}

	r := &Registry{schemas: make(map[string]*jsonschema.Schema)}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		typeName := strings.TrimSuffix(e.Name(), ".json")
		path := filepath.Join(dir, e.Name())

		s, err := compileFile(path)
		if err != nil {
			return nil, fmt.Errorf("termschema.Load: compile %q: %w", path, err)
		}
		r.schemas[typeName] = s
	}
	return r, nil
}

// compileFile opens a JSON schema file and compiles it with AssertFormat enabled.
func compileFile(path string) (*jsonschema.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource(path, doc); err != nil {
		return nil, fmt.Errorf("add resource: %w", err)
	}
	s, err := c.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	return s, nil
}

// Validate validates a Terms payload against the registered schema for t.Type.
// It returns an error if t.Type is not registered (unknown types are invalid —
// law 6) or if the payload does not satisfy the schema.
//
// The instance is constructed as map[string]any{"type": t.Type, "value": <value>}
// where value is t.Value unmarshalled from JSON into any.
func (r *Registry) Validate(t protocol.Terms) error {
	s, ok := r.schemas[t.Type]
	if !ok {
		return fmt.Errorf("termschema: unknown term type %q (registry is closed)", t.Type)
	}

	// Unmarshal the raw value into any so the jsonschema library can traverse it.
	var value any
	if err := json.Unmarshal(t.Value, &value); err != nil {
		return fmt.Errorf("termschema: unmarshal value for type %q: %w", t.Type, err)
	}

	instance := map[string]any{
		"type":  t.Type,
		"value": value,
	}

	if err := s.Validate(instance); err != nil {
		return fmt.Errorf("termschema: invalid %q payload: %w", t.Type, err)
	}
	return nil
}

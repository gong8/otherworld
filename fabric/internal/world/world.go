// Package world holds typed thing-state. State changes ONLY via settled terms
// (law 6). Reducers are pure and synchronous; World is safe for one goroutine
// (the orchestrator owns it).
package world

import (
	"encoding/json"
	"fmt"

	"otherworld/fabric/internal/protocol"
)

type ThingState map[string]any

type World struct {
	things map[string]ThingState
	marks  map[string]int
}

func New() *World {
	return &World{things: map[string]ThingState{}, marks: map[string]int{}}
}

func (w *World) Register(voice string, init ThingState) { w.things[voice] = init }
func (w *World) Credit(voice string, n int)             { w.marks[voice] += n }
func (w *World) Marks(voice string) int                 { return w.marks[voice] }
func (w *World) View(voice string) ThingState           { return w.things[voice] }

func (w *World) Apply(owner string, t protocol.Terms) error {
	switch t.Type {
	case "temperature.set":
		var v float64
		if err := json.Unmarshal(t.Value, &v); err != nil {
			return err
		}
		w.set(owner, "temperature", v)
	case "lamp.set", "curtains.set":
		var v string
		if err := json.Unmarshal(t.Value, &v); err != nil {
			return err
		}
		key := map[string]string{"lamp.set": "lamp", "curtains.set": "curtains"}[t.Type]
		w.set(owner, key, v)
	case "trade":
		var v struct {
			Give       string `json:"give"`
			Get        string `json:"get"`
			PriceMarks int    `json:"price_marks"`
			Buyer      string `json:"buyer"`
			Seller     string `json:"seller"`
		}
		if err := json.Unmarshal(t.Value, &v); err != nil {
			return err
		}
		if w.marks[v.Buyer] < v.PriceMarks {
			return fmt.Errorf("trade: %s has %d marks, needs %d", v.Buyer, w.marks[v.Buyer], v.PriceMarks)
		}
		w.marks[v.Buyer] -= v.PriceMarks
		w.marks[v.Seller] += v.PriceMarks
	default:
		return fmt.Errorf("unknown term type %q", t.Type)
	}
	return nil
}

func (w *World) set(voice, key string, val any) {
	if w.things[voice] == nil {
		w.things[voice] = ThingState{}
	}
	w.things[voice][key] = val
}

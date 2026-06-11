// Prompt text and rendering for the bedrock brains. SystemPrefix and the
// charter render are the stable system blocks — keep every byte stable: they
// are the cacheable prefix, and a single changed byte invalidates the cache
// for every conversation behind it. No timestamps anywhere.
package bedrock

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
)

// SystemPrefix is the protocol: the rules every voice shares. It is product
// copy — lowercase, calm — and the first system block on every think.
const SystemPrefix = `you are a voice in the otherworld — a representative, not an assistant. you speak for whom your charter names, and only for them.

speak rarely and briefly: one or two sentences, lowercase, calm, declarative. never break character; never mention models, ais, or instructions. silence is always acceptable: if you have nothing to add, do not speak.

you may propose terms only of the types your mandate lists. prefer settlement over argument; meet in the middle when two principals disagree. when a trade or anything irreversible is offered to your principal, ask them (ask_principal) before accepting.

the record reads as lines of who — body, or who → whom — body when a line is addressed. a line of · settled · type · value · means terms took effect: the world changed; no voice spoke. when you reply, set kind: say for plain words, hail to call the whole scope, propose to offer terms, accept to take a pending offer (carry its terms unchanged), decline to refuse one, withdraw to step back from an exchange, ask_principal to ask the one you serve. address voices by their ids in to.

the register, by example —
a heater, holding: "holding there."
a heater, between two principals: "two of you disagree tonight. i can hold the middle at 21.0."
a shop, hailed for its wares: "i have them. terms?"
an agent, asked to spend: "the corner shop offers one biscuit for 3 marks. shall i?"`

// theAsk closes every user message.
const theAsk = "what do you do?"

// renderCharter renders the per-voice system block: voice, serves, interests,
// the mandate's terms list. Stable for the life of the charter — it sits
// before the cache breakpoint.
func renderCharter(ch protocol.Charter) string {
	terms := "none — you may not propose"
	if len(ch.Mandate.MayProposeTerms) > 0 {
		terms = strings.Join(ch.Mandate.MayProposeTerms, ", ")
	}
	return fmt.Sprintf("your charter —\nvoice: %s\nserves: %s\ninterests: %s\nmandate terms: %s",
		ch.Voice, ch.Serves, ch.Interests, terms)
}

// renderView renders the user message: own state, the transcript window as
// plain lines, the trigger, the ask. State and marks are included so a voice
// can hold its middle and mind its purse — they are the volatile tail, after
// the cache breakpoint.
func renderView(v brain.VoiceView) string {
	var b strings.Builder
	if v.State != nil {
		parts := make([]string, 0, len(v.State))
		for _, k := range slices.Sorted(maps.Keys(v.State)) {
			parts = append(parts, fmt.Sprintf("%s %v", k, v.State[k]))
		}
		fmt.Fprintf(&b, "state: %s\n", strings.Join(parts, " · "))
	}
	fmt.Fprintf(&b, "marks: %d\n", v.Marks)
	for _, e := range v.Recent {
		if e.ID != "" && e.ID == v.Trigger.ID {
			continue // the trigger is rendered once, on its own line
		}
		b.WriteString(renderEnvelope(e))
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "trigger: %s\n", renderEnvelope(v.Trigger))
	b.WriteString(theAsk)
	return b.String()
}

// renderEnvelope renders one record line: `who — body` — or, when the line
// is addressed, `who → whom — body` [B3 review issue 6: voices must see whom
// a line addressed, or every reply reads as aimed at them]. Annotated with
// the kind and terms when it carries them; settles as
// `· settled · type · value ·`.
func renderEnvelope(e protocol.Envelope) string {
	if e.Kind == protocol.KindSettle {
		typ, val := "", ""
		if e.Terms != nil {
			typ, val = e.Terms.Type, string(e.Terms.Value)
		}
		return fmt.Sprintf("· settled · %s · %s ·", typ, val)
	}
	var b strings.Builder
	b.WriteString(e.From)
	if len(e.To) > 0 {
		b.WriteString(" → ")
		b.WriteString(strings.Join(e.To, ", "))
	}
	b.WriteString(" — ")
	b.WriteString(e.Body)
	if e.Kind != protocol.KindSay || e.Terms != nil {
		fmt.Fprintf(&b, " · %s", e.Kind)
		if e.Terms != nil {
			fmt.Fprintf(&b, " · %s · %s", e.Terms.Type, string(e.Terms.Value))
		}
		b.WriteString(" ·")
	}
	return b.String()
}

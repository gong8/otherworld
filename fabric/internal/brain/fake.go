package brain

import "context"

// Rule drives deterministic, scriptable voices for tests, local dev, and
// offline demos. First matching rule wins. Match must be a pure function of
// VoiceView — it is called more than once per utterance.
type Rule struct {
	Match   func(VoiceView) bool
	Respond func(VoiceView) Action
}

type Fake struct{ rules []Rule }

func NewFake(rules []Rule) *Fake { return &Fake{rules: rules} }

var _ Brain = (*Fake)(nil)

func (f *Fake) Relevant(_ context.Context, v VoiceView) (bool, error) {
	for _, r := range f.rules {
		if r.Match(v) {
			return true, nil
		}
	}
	return false, nil
}

func (f *Fake) Think(_ context.Context, v VoiceView) (Action, error) {
	for _, r := range f.rules {
		if r.Match(v) {
			return r.Respond(v), nil
		}
	}
	return Action{}, nil
}

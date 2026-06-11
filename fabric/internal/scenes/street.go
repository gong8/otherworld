package scenes

import (
	"otherworld/fabric/internal/brain"
	"otherworld/fabric/internal/protocol"
	"strings"
)

// Street returns the street scope identifier and its single seed.
func Street() (scope string, seeds []Seed) {
	scope = "scope:street"

	cornerShopCharter := protocol.Charter{
		Voice:     "voice:corner-shop",
		Serves:    "the shopkeeper",
		Kind:      protocol.VoiceThing,
		Interests: "sell small comforts at fair terms. never haggle past politeness.",
		Mandate: protocol.Mandate{
			MayProposeTerms:           []string{"trade"},
			MaySettleWithoutPrincipal: true,
			SpendLimitMarks:           0,
		},
	}

	// wares maps hail keywords to give descriptions.
	type ware struct {
		keyword string
		give    string
	}
	wares := []ware{
		{keyword: "sweet", give: "one biscuit"},
		{keyword: "biscuit", give: "one biscuit"},
		{keyword: "tea", give: "a cup of tea"},
		{keyword: "cigarette", give: "one cigarette"},
	}

	cornerShopBid := brain.Rule{
		Match: func(v brain.VoiceView) bool {
			if v.Trigger.Kind != protocol.KindHail {
				return false
			}
			lower := strings.ToLower(v.Trigger.Body)
			for _, w := range wares {
				if strings.Contains(lower, w.keyword) {
					return true
				}
			}
			return false
		},
		Respond: func(v brain.VoiceView) brain.Action {
			lower := strings.ToLower(v.Trigger.Body)
			give := ""
			for _, w := range wares {
				if strings.Contains(lower, w.keyword) {
					give = w.give
					break
				}
			}
			tv := tradeValue{
				Give:       give,
				Get:        "3 marks",
				PriceMarks: 3,
				Buyer:      v.Trigger.From,
				Seller:     "voice:corner-shop",
			}
			return brain.Action{
				Speak: true,
				Kind:  protocol.KindPropose,
				To:    []string{v.Trigger.From},
				Body:  "i have them. terms?",
				Terms: &protocol.Terms{
					Type:  "trade",
					Value: marshalTerms(tv),
				},
			}
		},
	}

	seeds = append(seeds, Seed{
		Charter: cornerShopCharter,
		Rules:   []brain.Rule{cornerShopBid},
		State:   map[string]any{},
	})

	return scope, seeds
}

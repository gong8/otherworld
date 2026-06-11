// Package protocol defines the otherworld wire types. proto/*.schema.json is
// the source of truth; agreement_test.go proves these types conform.
package protocol

import (
	"encoding/json"
	"time"
)

type Kind string

const (
	KindSay          Kind = "say"
	KindHail         Kind = "hail"
	KindPropose      Kind = "propose"
	KindAccept       Kind = "accept"
	KindDecline      Kind = "decline"
	KindWithdraw     Kind = "withdraw"
	KindAskPrincipal Kind = "ask_principal"
	KindSettle       Kind = "settle"
)

type VoiceKind string

const (
	VoicePerson VoiceKind = "person"
	VoiceThing  VoiceKind = "thing"
)

type Terms struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Envelope struct {
	V        int       `json:"v"`
	ID       string    `json:"id"`
	TS       time.Time `json:"ts"`
	From     string    `json:"from"`
	Serves   string    `json:"serves"`
	Scope    string    `json:"scope"`
	To       []string  `json:"to,omitempty"`
	Kind     Kind      `json:"kind"`
	Exchange string    `json:"exchange,omitempty"`
	Body     string    `json:"body,omitempty"`
	Terms    *Terms    `json:"terms,omitempty"`
}

type Mandate struct {
	MayProposeTerms           []string `json:"may_propose_terms"`
	MaySettleWithoutPrincipal bool     `json:"may_settle_without_principal"`
	SpendLimitMarks           int      `json:"spend_limit_marks"`
}

type Charter struct {
	Voice     string    `json:"voice"`
	Serves    string    `json:"serves"`
	Kind      VoiceKind `json:"kind"`
	Interests string    `json:"interests"`
	Mandate   Mandate   `json:"mandate"`
}

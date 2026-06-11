package orchestrator

import "otherworld/fabric/internal/protocol"

// WouldSettle exposes settleTarget's verdict for the equivalence test only:
// the consent/spend gates predict settlement through settleTarget while the
// lifecycle decides it for real, and any drift between the two is an
// economic-boundary bug (a gated accept settling ungated, or a free accept
// gated). TestSettleTargetMatchesLifecycle is the tripwire.
func (o *Orchestrator) WouldSettle(env protocol.Envelope) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.settleTarget(env) != nil
}

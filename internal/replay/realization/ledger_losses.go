package realization

import (
	"encoding/json"
	"strings"

	"planisphere/internal/sources/journal"
)

func (builder *ledgerBuilder) recordResurrect(rawJSON string) {
	var resurrectEvent journal.ResurrectEvent
	if json.Unmarshal([]byte(rawJSON), &resurrectEvent) != nil {
		return
	}
	builder.session.ApplyResurrect(resurrectEvent.Option)

	for _, organicRecord := range builder.unsoldOrganic {
		if organicRecord.status == "pending" {
			organicRecord.status = "lost_death"
			organicRecord.deathLoss = organicRecord.theoreticalTotal
		}
	}
	builder.unsoldOrganic = builder.unsoldOrganic[:0]

	if isShipDestructionResurrect(resurrectEvent) {
		for _, cartoRecord := range builder.unsoldCarto {
			if cartoRecord.status == "pending" {
				cartoRecord.status = "lost_death"
				cartoRecord.deathLoss = cartoRecord.theoreticalTotal
			}
		}
		clear(builder.unsoldCarto)
		clear(builder.unsoldCartoBySystem)
	}
}

// isShipDestructionResurrect reports whether a Resurrect journal event
// represents respawning from ship destruction (which destroys unsold shipboard
// cartographic data) rather than recovering from an on-foot suit death or SRV
// destruction (which preserves shipboard cartographic data).
//
// Loss accounting triggers on Resurrect rather than Died because no scans or
// sales can occur while the commander is on the rebuy or redeployment screen,
// and Died events for on-foot environmental hazards (heat, cold, or oxygen
// depletion) omit killer identifiers. Non-ship recovery options ("recover",
// "rejoin", "escape", "handin", or "free" with zero cost and Bankrupt false)
// preserve cartographic data, including "handin" where Cost > 0 reflects an
// on-foot detention-center bounty fine rather than ship insurance. Ship
// replacement options ("rebuy", "freewinder", Bankrupt true, or positive
// insurance cost) mark unsold cartography as lost.
//
// Edge case: if a commander who can afford their ship insurance (Bankrupt is
// false) voluntarily declines rebuy to forfeit their ship for a free starter
// Sidewinder and the journal records Option "free" with Cost 0 instead of
// "freewinder", Resurrect alone cannot distinguish that voluntary forfeit from
// a free on-foot redeployment to orbit.
func isShipDestructionResurrect(resurrectEvent journal.ResurrectEvent) bool {
	option := strings.ToLower(strings.TrimSpace(resurrectEvent.Option))
	if option == "handin" || option == "recover" || option == "rejoin" || option == "escape" {
		return false
	}
	return option == "rebuy" || option == "freewinder" || resurrectEvent.Bankrupt || resurrectEvent.Cost > 0
}

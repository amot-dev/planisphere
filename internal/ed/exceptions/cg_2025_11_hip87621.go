package exceptions

import "planisphere/internal/ed"

// HIP 87621 Exobiology Initiative Community Goal (November 2025).
// During the event, Vista Genomics aboard Exogene Sciences in HIP 87621 paid an
// 8x base value multiplier on Radicoida Unica (952,296 Cr per sample instead of
// its standard 119,037 Cr base value) and lifted the single-sample limit.
const (
	HIP87621SystemAddress         int64 = 147882789259
	ExogeneSciencesMarketID       int64 = 129038712
	HIP87621CGStartTimestamp            = "2025-11-13T00:00:00Z"
	HIP87621CGEndTimestamp              = "2025-11-28T00:00:00Z"
	hip87621RadicoidaCGMultiplier int64 = 8
)

func init() {
	RegisterOrganicRule(OrganicRule{
		ID:       "cg_2025_11_hip87621",
		Priority: 100,
		Evaluate: evaluateHIP87621RadicoidaCG,
	})
}

func evaluateHIP87621RadicoidaCG(context OrganicContext) (ed.ScanValue, bool) {
	if context.ResolvedSpeciesKey() != "ingensradices_unicus" {
		return ed.ScanValue{}, false
	}
	if context.SaleTimestamp != "" {
		if context.SaleMarketID != ExogeneSciencesMarketID ||
			context.SaleTimestamp < HIP87621CGStartTimestamp ||
			context.SaleTimestamp > HIP87621CGEndTimestamp {
			return ed.ScanValue{}, false
		}
	} else {
		if context.SystemAddress != HIP87621SystemAddress ||
			context.ScanTimestamp < HIP87621CGStartTimestamp ||
			context.ScanTimestamp > HIP87621CGEndTimestamp {
			return ed.ScanValue{}, false
		}
	}

	standardBaseValue := ed.SpeciesBaseValue(context.SpeciesCodex, context.SpeciesLocalised, context.GenusCodex)
	cgBaseValue := standardBaseValue * hip87621RadicoidaCGMultiplier
	return ed.OrganicValue(cgBaseValue, context.FirstLogged), true
}


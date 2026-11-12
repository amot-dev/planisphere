// Package exceptions isolates historical era pricing and post-event Community
// Goal valuation overrides from permanent Elite Dangerous constants.
package exceptions

import (
	"cmp"
	"slices"

	"planisphere/internal/ed"
)

// OrganicContext carries the scan and optional sale metadata needed to evaluate
// historical era pricing and post-event Community Goal overrides.
type OrganicContext struct {
	ScanTimestamp    string
	SaleTimestamp    string
	SystemAddress    int64
	SaleMarketID     int64
	SpeciesCodex     string
	SpeciesLocalised string
	GenusCodex       string
	FirstLogged      bool
}

// EffectiveTimestamp returns SaleTimestamp when the sample has been sold at
// Vista Genomics, falling back to ScanTimestamp while unsold or lost. Frontier
// evaluates era rebalances at redemption time rather than scan time.
func (context OrganicContext) EffectiveTimestamp() string {
	if context.SaleTimestamp != "" {
		return context.SaleTimestamp
	}
	return context.ScanTimestamp
}

// ResolvedSpeciesKey resolves the lowercase codex species key (for example,
// "stratum_07" or "ingensradices_unicus") using either the codex identifier or
// the English localised name.
func (context OrganicContext) ResolvedSpeciesKey() string {
	if foundSpecies, found := ed.LookupSpecies(context.SpeciesCodex, context.SpeciesLocalised); found {
		return ed.CodexKey(foundSpecies.CodexID)
	}
	return ed.CodexKey(context.SpeciesCodex)
}

// OrganicRule defines a registered valuation exception. Higher Priority rules
// (such as specific Community Goals) run before lower Priority rules (such as
// broad historical eras).
type OrganicRule struct {
	ID       string
	Priority int
	Evaluate func(context OrganicContext) (ed.ScanValue, bool)
}

var registeredOrganicRules []OrganicRule

// RegisterOrganicRule registers an exobiology valuation override and maintains
// descending priority order across registered rules.
func RegisterOrganicRule(rule OrganicRule) {
	registeredOrganicRules = append(registeredOrganicRules, rule)
	slices.SortStableFunc(registeredOrganicRules, func(leftRule, rightRule OrganicRule) int {
		return cmp.Compare(rightRule.Priority, leftRule.Priority)
	})
}

// EvaluateOrganic returns the exobiology payout for context, applying the
// highest-priority matching exception rule or falling back to standard Update 14
// Vista Genomics pricing when no exception applies.
func EvaluateOrganic(context OrganicContext) ed.ScanValue {
	for _, registeredRule := range registeredOrganicRules {
		if overrideValue, matched := registeredRule.Evaluate(context); matched {
			return overrideValue
		}
	}
	standardBaseValue := ed.SpeciesBaseValue(context.SpeciesCodex, context.SpeciesLocalised, context.GenusCodex)
	return ed.OrganicValue(standardBaseValue, context.FirstLogged)
}


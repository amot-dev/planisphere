package ed

// First-bonus eligibility predicates for Universal Cartographics and Vista Genomics.
//
// Inhabited systems (Population > 0) in the Bubble and Colonia disable First
// Discovered, First Mapped, and First Footfall tags in-game while leaving
// WasDiscovered and WasFootfalled locked to false in journal Scan events. By
// contrast, unpopulated systems (Population == 0, including near-Bubble Road to
// Riches worlds) update discovery and footfall flags normally once sold.

// IsTagLockedBubbleBody reports whether a body exhibits Frontier's tag-locked
// signature (!wasDiscovered and wasMapped). In normal exploration a body cannot
// be mapped with a Detailed Surface Scanner without first being discovered, so
// WasDiscovered false paired with WasMapped true indicates a pre-known Bubble
// world whose First Discovery tag is permanently disabled.
func IsTagLockedBubbleBody(wasDiscovered, wasMapped bool) bool {
	return !wasDiscovered && wasMapped
}

// IsSystemBonusEligible reports whether bodies in a system are eligible to
// award First Discovered, First Mapped, or First Logged bonuses based on the
// parent system's population. Unpopulated systems (systemPopulation == 0) are
// eligible, whereas inhabited systems suppress first-discovery tags.
func IsSystemBonusEligible(systemPopulation int64) bool {
	return systemPopulation == 0
}

// CanExpectFirstDiscovered reports whether scanning a body can earn a First
// Discovery bonus at Universal Cartographics.
func CanExpectFirstDiscovered(systemPopulation int64, wasDiscovered, wasMapped bool) bool {
	return IsSystemBonusEligible(systemPopulation) &&
		!IsTagLockedBubbleBody(wasDiscovered, wasMapped) &&
		!wasDiscovered
}

// CanExpectFirstMapped reports whether mapping a body with a Detailed Surface
// Scanner can earn a First Mapped bonus at Universal Cartographics.
func CanExpectFirstMapped(systemPopulation int64, wasDiscovered, wasMapped bool) bool {
	return IsSystemBonusEligible(systemPopulation) &&
		!IsTagLockedBubbleBody(wasDiscovered, wasMapped) &&
		!wasMapped
}

// CanExpectFirstLogged reports whether sampling an organic species on a body
// can be expected at scan time to earn Vista Genomics' First Logged bonus.
// When wasFootfalled is present on the parent Scan event, an un-footfalled
// body (false) expects the bonus. When wasFootfalled is nil in pre-2025
// journals recorded before Frontier added WasFootfalled to Scan, a completely
// untouched body (!wasDiscovered and !wasMapped) serves as the scan-time proxy.
func CanExpectFirstLogged(systemPopulation int64, wasDiscovered, wasMapped bool, wasFootfalled *bool) bool {
	if !IsSystemBonusEligible(systemPopulation) || IsTagLockedBubbleBody(wasDiscovered, wasMapped) {
		return false
	}
	if wasFootfalled != nil {
		return !*wasFootfalled
	}
	return !wasDiscovered && !wasMapped
}


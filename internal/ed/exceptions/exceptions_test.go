package exceptions

import (
	"testing"

	"planisphere/internal/ed"
)

func TestEvaluateOrganicExceptions(t *testing.T) {
	testCases := []struct {
		name    string
		context OrganicContext
		want    ed.ScanValue
	}{
		{
			name: "pre-Update-14 first-logged sample sold before Update 14 uses 2x multiplier",
			context: OrganicContext{
				ScanTimestamp:    "2022-09-20T02:08:22Z",
				SaleTimestamp:    "2022-10-05T00:11:12Z",
				SpeciesCodex:     "$Codex_Ent_Clypeus_03_Name;",
				SpeciesLocalised: "Clypeus Speculumi",
				FirstLogged:      true,
			},
			want: ed.ScanValue{Base: 711500, Bonus: 711500, Total: 1423000},
		},
		{
			name: "pre-Update-7 Tubus Rosarium launch value matches 2021 journal payout",
			context: OrganicContext{
				ScanTimestamp:    "2021-08-28T01:10:30Z",
				SaleTimestamp:    "2021-08-28T01:36:25Z",
				SpeciesCodex:     "$Codex_Ent_Tubus_04_Name;",
				SpeciesLocalised: "Tubus Rosarium",
				FirstLogged:      false,
			},
			want: ed.ScanValue{Base: 400500, Bonus: 0, Total: 400500},
		},
		{
			name: "sample scanned before Update 14 and redeemed after Update 14 uses Update 14 pricing",
			context: OrganicContext{
				ScanTimestamp:    "2022-10-22T03:46:34Z",
				SaleTimestamp:    "2022-12-10T07:53:40Z",
				SpeciesCodex:     "$Codex_Ent_Stratum_07_Name;",
				SpeciesLocalised: "Stratum Tectonicas",
				FirstLogged:      true,
			},
			want: ed.ScanValue{Base: 19010800, Bonus: 76043200, Total: 95054000},
		},
		{
			name: "Radicoida Unica sold at Exogene Sciences during HIP 87621 CG uses 8x base",
			context: OrganicContext{
				ScanTimestamp:    "2025-11-24T18:46:36Z",
				SaleTimestamp:    "2025-11-24T19:12:26Z",
				SystemAddress:    HIP87621SystemAddress,
				SaleMarketID:     ExogeneSciencesMarketID,
				SpeciesCodex:     "$Codex_Ent_Ingensradices_Unicus_Name;",
				SpeciesLocalised: "Radicoida Unica",
				FirstLogged:      false,
			},
			want: ed.ScanValue{Base: 952296, Bonus: 0, Total: 952296},
		},
		{
			name: "Radicoida Unica scanned after HIP 87621 CG uses permanent 119037 Cr base",
			context: OrganicContext{
				ScanTimestamp:    "2026-01-15T10:00:00Z",
				SystemAddress:    HIP87621SystemAddress,
				SpeciesCodex:     "$Codex_Ent_Ingensradices_Unicus_Name;",
				SpeciesLocalised: "Radicoida Unica",
				FirstLogged:      false,
			},
			want: ed.ScanValue{Base: 119037, Bonus: 0, Total: 119037},
		},
	}

	for _, testCase := range testCases {
		actualValue := EvaluateOrganic(testCase.context)
		if actualValue != testCase.want {
			t.Errorf("%s: EvaluateOrganic = %+v, want %+v", testCase.name, actualValue, testCase.want)
		}
	}
}


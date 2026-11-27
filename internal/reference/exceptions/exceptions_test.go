package exceptions

import (
	"testing"

	"planisphere/internal/reference"
)

func TestEvaluateOrganicExceptions(t *testing.T) {
	testCases := []struct {
		name    string
		context OrganicContext
		want    reference.ScanValue
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
			want: reference.ScanValue{Base: 711500, Bonus: 711500, Total: 1423000},
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
			want: reference.ScanValue{Base: 400500, Bonus: 0, Total: 400500},
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
			want: reference.ScanValue{Base: 19010800, Bonus: 76043200, Total: 95054000},
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
			want: reference.ScanValue{Base: 952296, Bonus: 0, Total: 952296},
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
			want: reference.ScanValue{Base: 119037, Bonus: 0, Total: 119037},
		},
		{
			name: "missing effective timestamp skips era rule and falls back to Update 14 pricing",
			context: OrganicContext{
				SpeciesCodex:     "$Codex_Ent_Stratum_07_Name;",
				SpeciesLocalised: "Stratum Tectonicas",
				FirstLogged:      false,
			},
			want: reference.ScanValue{Base: 19010800, Bonus: 0, Total: 19010800},
		},
		{
			name: "post-Update-7 pre-Update-14 Tubus Rosarium uses rebalanced 171900 Cr tier",
			context: OrganicContext{
				ScanTimestamp:    "2022-02-14T12:00:00Z",
				SpeciesCodex:     "$Codex_Ent_Tubus_04_Name;",
				SpeciesLocalised: "Tubus Rosarium",
				FirstLogged:      false,
			},
			want: reference.ScanValue{Base: 171900, Bonus: 0, Total: 171900},
		},
		{
			name: "pre-Update-14 unknown numbered species falls back to stripped genus prefix",
			context: OrganicContext{
				ScanTimestamp: "2022-05-01T10:00:00Z",
				SpeciesCodex:  "$Codex_Ent_Stratum_99_Name;",
				FirstLogged:   false,
			},
			want: reference.ScanValue{Base: 102500, Bonus: 0, Total: 102500},
		},
		{
			name: "pre-Update-14 non-numbered codex key falls back to direct genus map",
			context: OrganicContext{
				ScanTimestamp: "2022-05-01T10:00:00Z",
				SpeciesCodex:  "$Codex_Ent_Vents_Name;",
				FirstLogged:   true,
			},
			want: reference.ScanValue{Base: 117900, Bonus: 117900, Total: 235800},
		},
		{
			name: "pre-Update-14 unrecognized species key falls back to GenusCodex suffix trim",
			context: OrganicContext{
				ScanTimestamp: "2022-05-01T10:00:00Z",
				SpeciesCodex:  "$Codex_Ent_MysteryBio_Alpha_Name;",
				GenusCodex:    "$Codex_Ent_Cone_Genus_Name;",
				FirstLogged:   false,
			},
			want: reference.ScanValue{Base: 108900, Bonus: 0, Total: 108900},
		},
		{
			name: "pre-Update-14 completely unknown species and genus returns zero value",
			context: OrganicContext{
				ScanTimestamp: "2022-05-01T10:00:00Z",
				SpeciesCodex:  "$Codex_Ent_UnknownBio_01_Name;",
				GenusCodex:    "$Codex_Ent_UnknownBio_Genus_Name;",
				FirstLogged:   true,
			},
			want: reference.ScanValue{},
		},
	}

	for _, testCase := range testCases {
		actualValue := EvaluateOrganic(testCase.context)
		if actualValue != testCase.want {
			t.Errorf("%s: EvaluateOrganic = %+v, want %+v", testCase.name, actualValue, testCase.want)
		}
	}
}

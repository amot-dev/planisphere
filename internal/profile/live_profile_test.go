//go:build profile

package profile_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"planisphere/internal/profile"
)

func TestLiveDataProfile(t *testing.T) {
	iterations := 3
	if rawIter := strings.TrimSpace(os.Getenv("PROFILE_ITER")); rawIter != "" {
		if parsed, parseErr := strconv.Atoi(rawIter); parseErr == nil && parsed > 0 {
			iterations = parsed
		}
	}
	includeFrontend := strings.TrimSpace(os.Getenv("PROFILE_SKIP_FRONTEND")) != "1"

	options := profile.Options{
		DBPath:          strings.TrimSpace(os.Getenv("ED_PLANNER_DB")),
		FilterPattern:   strings.TrimSpace(os.Getenv("PROFILE_FILTER")),
		Iterations:      iterations,
		IncludeFrontend: includeFrontend,
		SavePath:        strings.TrimSpace(os.Getenv("PROFILE_SAVE")),
		BaselinePath:    strings.TrimSpace(os.Getenv("PROFILE_BASELINE")),
	}

	report, runError := profile.RunSnapshotSuite(context.Background(), options)
	if runError != nil {
		t.Fatalf("RunSnapshotSuite failed: %v", runError)
	}

	var diffReport *profile.DiffReport
	if options.BaselinePath != "" {
		baseline, loadErr := profile.LoadReport(options.BaselinePath)
		if loadErr != nil {
			t.Fatalf("load baseline %s: %v", options.BaselinePath, loadErr)
		}
		computedDiff := profile.CompareReports(baseline, report)
		diffReport = &computedDiff
	}

	if options.SavePath != "" {
		if saveErr := profile.SaveReport(options.SavePath, report); saveErr != nil {
			t.Fatalf("save report %s: %v", options.SavePath, saveErr)
		}
	}

	formatted := profile.FormatReport(report, diffReport)
	fmt.Print(formatted)

	for _, probe := range report.Probes {
		if probe.HasWarm && !probe.ColdWarmMatch {
			t.Errorf("probe %s produced different result hashes between Cold (%s) and Warm (%s)", probe.Name, probe.Cold.ResultHash, probe.Warm.ResultHash)
		}
	}
	if diffReport != nil && diffReport.AnyResultMismatch {
		t.Errorf("one or more probes produced a result hash mismatch compared to baseline %s", options.BaselinePath)
	}
}


package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"planisphere/internal/core/db"
)

func runSnapshotDB(args []string) error {
	flagSet := flag.NewFlagSet("snapshot-db", flag.ContinueOnError)
	outputFlag := flagSet.String("out", "", "destination snapshot path (defaults to OS temp directory)")
	sourceFlag := flagSet.String("src", "", "explicit source database path override")
	if parseError := flagSet.Parse(args); parseError != nil {
		return parseError
	}

	sourcePath, resolveError := resolveSnapshotSourcePath(*sourceFlag)
	if resolveError != nil {
		return fmt.Errorf("resolve source database path: %w", resolveError)
	}

	destinationPath := strings.TrimSpace(*outputFlag)
	if destinationPath == "" {
		destinationPath = filepath.Join(os.TempDir(), "planisphere-snap.db")
	}

	if snapshotError := db.SnapshotTo(sourcePath, destinationPath); snapshotError != nil {
		return snapshotError
	}

	fmt.Printf("[snapshot-db] Created snapshot: %s (source: %s)\n", destinationPath, sourcePath)
	return nil
}

func resolveSnapshotSourcePath(explicitSource string) (string, error) {
	if trimmedSource := strings.TrimSpace(explicitSource); trimmedSource != "" {
		return trimmedSource, nil
	}
	if envSource := strings.TrimSpace(os.Getenv("ED_PLANNER_DB")); envSource != "" {
		return envSource, nil
	}
	return db.ResolveConfiguredPath()
}

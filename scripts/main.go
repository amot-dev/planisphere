package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type scriptCommand func(args []string) error

var scriptCommands = map[string]scriptCommand{
	"sync-metadata": runSyncMetadata,
	"snapshot-db":   runSnapshotDB,
}

func main() {
	if len(os.Args) < 2 {
		printScriptUsage()
		os.Exit(1)
	}

	commandName := os.Args[1]
	commandFunc, exists := scriptCommands[commandName]
	if !exists {
		fmt.Fprintf(os.Stderr, "Unknown script command: %s\n", commandName)
		printScriptUsage()
		os.Exit(1)
	}

	if runError := commandFunc(os.Args[2:]); runError != nil {
		fmt.Fprintf(os.Stderr, "Error running %s: %v\n", commandName, runError)
		os.Exit(1)
	}
}

func printScriptUsage() {
	commandNames := make([]string, 0, len(scriptCommands))
	for commandName := range scriptCommands {
		commandNames = append(commandNames, commandName)
	}
	sort.Strings(commandNames)
	fmt.Fprintf(os.Stderr, "Usage: go run ./scripts <command> [flags]\nAvailable commands: %s\n", strings.Join(commandNames, ", "))
}

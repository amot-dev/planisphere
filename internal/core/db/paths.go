package db

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"planisphere/internal/version"
)

// AppDirName is the per-user directory holding the database and bootstrap configuration.
var AppDirName = version.ProductName

// DefaultDBFileName is the default database file name.
var DefaultDBFileName = version.BinaryName + ".db"

// UserDataDir returns the standard OS directory for user application data (databases, persistent state):
// - Linux/Unix: $XDG_DATA_HOME or ~/.local/share
// - Windows: %LOCALAPPDATA% or %USERPROFILE%\AppData\Local
// - macOS: ~/Library/Application Support
func UserDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return local, nil
		}
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, "AppData", "Local"), nil
	case "darwin":
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	default:
		if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
			return xdgDataHome, nil
		}
		home, homeDirError := os.UserHomeDir()
		if homeDirError != nil {
			return "", homeDirError
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}

// DefaultPath returns <UserDataDir>/Planisphere/planisphere.db.
func DefaultPath() (string, error) {
	dataDir, dataDirError := UserDataDir()
	if dataDirError != nil {
		return "", dataDirError
	}
	return filepath.Join(dataDir, AppDirName, DefaultDBFileName), nil
}

type bootstrapConfig struct {
	DBPath string `json:"dbPath"`
}

func bootstrapFilePath() (string, error) {
	configDir, configDirError := os.UserConfigDir()
	if configDirError != nil {
		return "", configDirError
	}
	return filepath.Join(configDir, AppDirName, "bootstrap.json"), nil
}

// ResolveConfiguredPath returns the user's configured database path from
// bootstrap.json if set, or DefaultPath() otherwise.
func ResolveConfiguredPath() (string, error) {
	if configFilePath, bootstrapError := bootstrapFilePath(); bootstrapError == nil {
		if configBytes, readError := os.ReadFile(configFilePath); readError == nil {
			var config bootstrapConfig
			if json.Unmarshal(configBytes, &config) == nil && strings.TrimSpace(config.DBPath) != "" {
				return filepath.Clean(strings.TrimSpace(config.DBPath)), nil
			}
		}
	}
	return DefaultPath()
}

// SaveConfiguredPath persists the user's chosen database path to bootstrap.json.
func SaveConfiguredPath(dbPath string) error {
	configFilePath, bootstrapError := bootstrapFilePath()
	if bootstrapError != nil {
		return bootstrapError
	}
	if mkdirError := os.MkdirAll(filepath.Dir(configFilePath), 0o755); mkdirError != nil {
		return mkdirError
	}
	encodedConfig, marshalError := json.MarshalIndent(bootstrapConfig{DBPath: filepath.Clean(strings.TrimSpace(dbPath))}, "", "  ")
	if marshalError != nil {
		return marshalError
	}
	return os.WriteFile(configFilePath, append(encodedConfig, '\n'), 0o644)
}


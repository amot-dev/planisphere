package eddiscovery

import (
	"os"
	"path/filepath"
	"runtime"
)

// DiscoverDBPath dynamically locates an existing EDDiscovery SQLite database
// (EDDUser.sqlite) using standard operating system data and configuration directories.
func DiscoverDBPath() string {
	homeDir, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	cacheDir, _ := os.UserCacheDir()
	xdgDataDir := os.Getenv("XDG_DATA_HOME")
	if xdgDataDir == "" && homeDir != "" {
		xdgDataDir = filepath.Join(homeDir, ".local", "share")
	}

	var candidateRoots []string
	for _, rootPath := range []string{xdgDataDir, cacheDir, configDir, os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")} {
		if rootPath != "" {
			candidateRoots = append(candidateRoots, rootPath)
		}
	}
	for _, rootPath := range candidateRoots {
		candidateFile := filepath.Join(rootPath, "EDDiscovery", "EDDUser.sqlite")
		if fileInfo, statError := os.Stat(candidateFile); statError == nil && !fileInfo.IsDir() {
			return candidateFile
		}
	}
	return ""
}

// DefaultDBPath returns an existing EDDUser.sqlite path if discovered, or the
// standard operating-system-specific default path where EDDiscovery stores EDDUser.sqlite.
func DefaultDBPath() string {
	if discoveredPath := DiscoverDBPath(); discoveredPath != "" {
		return discoveredPath
	}
	homeDir, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	switch runtime.GOOS {
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "EDDiscovery", "EDDUser.sqlite")
		}
		if homeDir != "" {
			return filepath.Join(homeDir, "AppData", "Local", "EDDiscovery", "EDDUser.sqlite")
		}
	case "darwin":
		if configDir != "" {
			return filepath.Join(configDir, "EDDiscovery", "EDDUser.sqlite")
		}
	default:
		xdgDataDir := os.Getenv("XDG_DATA_HOME")
		if xdgDataDir == "" && homeDir != "" {
			xdgDataDir = filepath.Join(homeDir, ".local", "share")
		}
		if xdgDataDir != "" {
			return filepath.Join(xdgDataDir, "EDDiscovery", "EDDUser.sqlite")
		}
	}
	return filepath.Join("EDDiscovery", "EDDUser.sqlite")
}


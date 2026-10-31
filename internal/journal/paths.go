package journal

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var vdfPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)

// steamLibraryRoots discovers Steam installation and library folders dynamically
// from environment variables, XDG directories, and Steam's libraryfolders.vdf.
func steamLibraryRoots(home string) []string {
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" && home != "" {
		xdgData = filepath.Join(home, ".local", "share")
	}

	var baseSteamDirs []string
	if xdgData != "" {
		baseSteamDirs = append(baseSteamDirs, filepath.Join(xdgData, "Steam"))
	}
	if home != "" {
		baseSteamDirs = append(baseSteamDirs,
			filepath.Join(home, ".steam", "steam"),
			filepath.Join(home, ".steam", "root"),
			filepath.Join(home, ".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam"),
			filepath.Join(home, "snap", "steam", "common", ".local", "share", "Steam"),
		)
	}

	seen := map[string]bool{}
	var roots []string
	addRoot := func(p string) {
		p = filepath.Clean(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		roots = append(roots, p)
	}

	for _, base := range baseSteamDirs {
		addRoot(base)
		for _, vdfRel := range []string{
			filepath.Join("steamapps", "libraryfolders.vdf"),
			filepath.Join("config", "libraryfolders.vdf"),
		} {
			data, err := os.ReadFile(filepath.Join(base, vdfRel))
			if err != nil {
				continue
			}
			for _, m := range vdfPathRe.FindAllSubmatch(data, -1) {
				libPath := strings.ReplaceAll(string(m[1]), `\\`, `\`)
				addRoot(libPath)
			}
		}
	}
	return roots
}

// DefaultJournalDirs returns candidate default journal directories for the
// current OS, discovered dynamically from OS environment and Steam library metadata.
func DefaultJournalDirs() []string {
	home, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	cacheDir, _ := os.UserCacheDir()

	switch runtime.GOOS {
	case "windows":
		var candidates []string
		for _, base := range []string{os.Getenv("USERPROFILE"), home} {
			if base != "" {
				candidates = append(candidates, filepath.Join(base, "Saved Games", "Frontier Developments", "Elite Dangerous"))
			}
		}
		if len(candidates) == 0 {
			candidates = append(candidates, filepath.Join("Saved Games", "Frontier Developments", "Elite Dangerous"))
		}
		return candidates

	case "darwin":
		if configDir != "" {
			return []string{filepath.Join(configDir, "Frontier Developments", "Elite Dangerous")}
		}
		return []string{filepath.Join(home, "Library", "Application Support", "Frontier Developments", "Elite Dangerous")}

	default:
		protonSuffix := filepath.Join("steamapps", "compatdata", "359320", "pfx", "drive_c", "users", "steamuser",
			"Saved Games", "Frontier Developments", "Elite Dangerous")
		var candidates []string
		for _, root := range steamLibraryRoots(home) {
			candidates = append(candidates, filepath.Join(root, protonSuffix))
		}
		// Also check common local Wine / Heroic / Lutris / Games locations if present.
		for _, base := range []string{home, configDir, cacheDir} {
			if base == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(base, "Games", "Elite Dangerous", "journals"),
				filepath.Join(base, ".wine", "drive_c", "users", os.Getenv("USER"), "Saved Games", "Frontier Developments", "Elite Dangerous"),
			)
		}
		if len(candidates) == 0 && home != "" {
			candidates = append(candidates, filepath.Join(home, protonSuffix))
		}
		return candidates
	}
}

// DefaultJournalDir returns the first existing default directory that contains
// journal files (or exists as a directory), or the primary candidate when none exists.
func DefaultJournalDir() string {
	dirs := DefaultJournalDirs()
	for _, d := range dirs {
		if matches, err := filepath.Glob(filepath.Join(d, JournalGlob)); err == nil && len(matches) > 0 {
			return d
		}
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	if len(dirs) > 0 {
		return dirs[0]
	}
	return ""
}

// DiscoverEDDiscoveryDBPath dynamically locates an existing EDDiscovery SQLite
// database (EDDUser.sqlite) using OS / XDG standard data and config directories.
func DiscoverEDDiscoveryDBPath() string {
	home, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	cacheDir, _ := os.UserCacheDir()
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" && home != "" {
		xdgData = filepath.Join(home, ".local", "share")
	}

	var roots []string
	for _, r := range []string{xdgData, cacheDir, configDir, os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")} {
		if r != "" {
			roots = append(roots, r)
		}
	}
	for _, r := range roots {
		p := filepath.Join(r, "EDDiscovery", "EDDUser.sqlite")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// DefaultEDDiscoveryDBPath returns an existing EDDUser.sqlite path if discovered,
// or the standard OS-specific default path where EDDiscovery stores EDDUser.sqlite.
func DefaultEDDiscoveryDBPath() string {
	if found := DiscoverEDDiscoveryDBPath(); found != "" {
		return found
	}
	home, _ := os.UserHomeDir()
	configDir, _ := os.UserConfigDir()
	switch runtime.GOOS {
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "EDDiscovery", "EDDUser.sqlite")
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Local", "EDDiscovery", "EDDUser.sqlite")
		}
	case "darwin":
		if configDir != "" {
			return filepath.Join(configDir, "EDDiscovery", "EDDUser.sqlite")
		}
	default:
		xdgData := os.Getenv("XDG_DATA_HOME")
		if xdgData == "" && home != "" {
			xdgData = filepath.Join(home, ".local", "share")
		}
		if xdgData != "" {
			return filepath.Join(xdgData, "EDDiscovery", "EDDUser.sqlite")
		}
	}
	return filepath.Join("EDDiscovery", "EDDUser.sqlite")
}

// DirExists reports whether dir exists and is a directory.
func DirExists(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}

// FileExists reports whether path exists and is a regular file.
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

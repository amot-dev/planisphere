package journal

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
)

var (
	vdfPathRe = regexp.MustCompile(`"path"\s+"([^"]+)"`)

	defaultJournalDirsOnce   sync.Once
	cachedDefaultJournalDirs []string

	defaultJournalDirOnce   sync.Once
	cachedDefaultJournalDir string
)

// steamLibraryRoots discovers Steam installation and library folders dynamically
// from environment variables, XDG directories, and Steam's libraryfolders.vdf.
func steamLibraryRoots(home string) []string {
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome == "" && home != "" {
		xdgDataHome = filepath.Join(home, ".local", "share")
	}

	var baseSteamDirs []string
	if xdgDataHome != "" {
		baseSteamDirs = append(baseSteamDirs, filepath.Join(xdgDataHome, "Steam"))
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
	addRoot := func(candidatePath string) {
		cleaned := filepath.Clean(candidatePath)
		if cleaned == "" || seen[cleaned] {
			return
		}
		seen[cleaned] = true
		roots = append(roots, cleaned)
	}

	for _, base := range baseSteamDirs {
		addRoot(base)
		for _, relativeVDFPath := range []string{
			filepath.Join("steamapps", "libraryfolders.vdf"),
			filepath.Join("config", "libraryfolders.vdf"),
		} {
			vdfBytes, readError := os.ReadFile(filepath.Join(base, relativeVDFPath))
			if readError != nil {
				continue
			}
			for _, match := range vdfPathRe.FindAllSubmatch(vdfBytes, -1) {
				libPath := strings.ReplaceAll(string(match[1]), `\\`, `\`)
				addRoot(libPath)
			}
		}
	}
	return roots
}

// DefaultJournalDirs returns candidate default journal directories for the
// current OS, discovered once per process from OS environment and Steam library metadata.
func DefaultJournalDirs() []string {
	defaultJournalDirsOnce.Do(func() {
		cachedDefaultJournalDirs = discoverDefaultJournalDirs()
	})
	return slices.Clone(cachedDefaultJournalDirs)
}

func discoverDefaultJournalDirs() []string {
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
// Discovery is cached once per process.
func DefaultJournalDir() string {
	defaultJournalDirOnce.Do(func() {
		cachedDefaultJournalDir = discoverDefaultJournalDir()
	})
	return cachedDefaultJournalDir
}

func discoverDefaultJournalDir() string {
	dirs := DefaultJournalDirs()
	for _, dirPath := range dirs {
		if matches, globError := filepath.Glob(filepath.Join(dirPath, JournalGlob)); globError == nil && len(matches) > 0 {
			return dirPath
		}
	}
	for _, dirPath := range dirs {
		if fileInfo, statError := os.Stat(dirPath); statError == nil && fileInfo.IsDir() {
			return dirPath
		}
	}
	if len(dirs) > 0 {
		return dirs[0]
	}
	return ""
}

// DirExists reports whether dir exists and is a directory.
func DirExists(dir string) bool {
	fileInfo, statError := os.Stat(dir)
	return statError == nil && fileInfo.IsDir()
}

// FileExists reports whether path exists and is a regular file.
func FileExists(path string) bool {
	fileInfo, statError := os.Stat(path)
	return statError == nil && !fileInfo.IsDir()
}

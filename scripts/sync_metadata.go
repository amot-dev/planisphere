package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Name        string `json:"name"`
	ProductName string `json:"productName"`
	Tagline     string `json:"tagline"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	BinaryName  string `json:"binaryName"`
	Repository  string `json:"repository"`
}

func main() {
	rootDir, findError := findRootDir()
	if findError != nil {
		fmt.Fprintf(os.Stderr, "Error locating root directory: %v\n", findError)
		os.Exit(1)
	}

	configPath := filepath.Join(rootDir, "app.config.json")
	configBytes, readError := os.ReadFile(configPath)
	if readError != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", configPath, readError)
		os.Exit(1)
	}

	var appConfig Config
	if parseError := json.Unmarshal(configBytes, &appConfig); parseError != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", configPath, parseError)
		os.Exit(1)
	}

	if appConfig.ProductName == "" {
		appConfig.ProductName = "Planisphere"
	}
	if appConfig.BinaryName == "" {
		appConfig.BinaryName = "planisphere"
	}
	appConfig.Repository = strings.TrimSuffix(strings.TrimSpace(appConfig.Repository), "/")
	if appConfig.Repository == "" {
		appConfig.Repository = "https://github.com/amot-dev/planisphere"
	}

	fmt.Printf("[sync-metadata] Reading app.config.json: %s v%s (%s)\n", appConfig.ProductName, appConfig.Version, appConfig.BinaryName)

	// Sync wails.json (application name, binary output, embedded version info)
	syncWailsJSON(filepath.Join(rootDir, "wails.json"), appConfig)

	// Sync frontend/package.json (name, version, description)
	syncPackageJSON(filepath.Join(rootDir, "frontend", "package.json"), appConfig)

	// Sync frontend/package-lock.json (name, version)
	syncPackageLockJSON(filepath.Join(rootDir, "frontend", "package-lock.json"), appConfig)

	// Keep node_modules stamp in sync if node_modules is already installed
	syncNodeModulesStamp(filepath.Join(rootDir, "frontend", "node_modules", ".package-lock.json"), appConfig)

	// Sync frontend/index.html (<title> banner)
	syncIndexHTML(filepath.Join(rootDir, "frontend", "index.html"), appConfig)

	// Generate internal/version/version.go (consumed directly by main.go and Go backend)
	generateGoVersion(filepath.Join(rootDir, "internal", "version", "version.go"), appConfig)

	// Generate frontend/src/version.ts (consumed by React UI)
	generateTSVersion(filepath.Join(rootDir, "frontend", "src", "version.ts"), appConfig)

	// Sync Linux desktop entry
	syncDesktopFile(filepath.Join(rootDir, "build", "linux", appConfig.BinaryName+".desktop"), appConfig)

	// Sync Arch PKGBUILD
	syncPKGBUILD(filepath.Join(rootDir, "packaging", "arch", "PKGBUILD"), appConfig)

	// Sync nfpm package manifest
	syncNFPM(filepath.Join(rootDir, "packaging", "nfpm.yaml"), appConfig)

	fmt.Printf("Metadata synchronized successfully across all project targets.\n")
}

func writeFileIfChanged(path string, newContent []byte, perm os.FileMode) (bool, error) {
	oldContent, err := os.ReadFile(path)
	if err == nil && bytes.Equal(oldContent, newContent) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, newContent, perm)
}

func findRootDir() (string, error) {
	currentDir, cwdError := os.Getwd()
	if cwdError != nil {
		return "", cwdError
	}
	dirCursor := currentDir
	for {
		if _, statError := os.Stat(filepath.Join(dirCursor, "app.config.json")); statError == nil {
			return dirCursor, nil
		}
		parentDir := filepath.Dir(dirCursor)
		if parentDir == dirCursor {
			break
		}
		dirCursor = parentDir
	}
	return ".", nil
}

func updateJSONFile(path string, warnOnError bool, mutate func(document map[string]any)) bool {
	fileBytes, readError := os.ReadFile(path)
	if readError != nil {
		if warnOnError {
			fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, readError)
		}
		return false
	}
	var document map[string]any
	if unmarshalError := json.Unmarshal(fileBytes, &document); unmarshalError != nil {
		return false
	}
	mutate(document)
	encodedBytes, marshalError := json.MarshalIndent(document, "", "  ")
	if marshalError != nil {
		return false
	}
	changed, writeError := writeFileIfChanged(path, append(encodedBytes, '\n'), 0644)
	if writeError != nil {
		if warnOnError {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", path, writeError)
		}
		return false
	}
	return changed
}

func syncWailsJSON(path string, appConfig Config) {
	if updateJSONFile(path, true, func(manifestMap map[string]any) {
		manifestMap["_comment"] = "DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json"
		manifestMap["_frontend:install_comment"] = "Frontend dependencies are managed incrementally by Makefile (install-frontend)"
		delete(manifestMap, "frontend:install")
		manifestMap["name"] = appConfig.Name
		manifestMap["outputfilename"] = appConfig.BinaryName
		if author, ok := manifestMap["author"].(map[string]any); ok {
			author["name"] = appConfig.Author
		} else {
			manifestMap["author"] = map[string]any{"name": appConfig.Author}
		}
		if info, ok := manifestMap["info"].(map[string]any); ok {
			info["productName"] = appConfig.ProductName
			info["productVersion"] = appConfig.Version
			info["comments"] = appConfig.Description
		} else {
			manifestMap["info"] = map[string]any{
				"productName":    appConfig.ProductName,
				"productVersion": appConfig.Version,
				"comments":       appConfig.Description,
			}
		}
	}) {
		fmt.Printf("  -> Updated wails.json (name: %q, binary: %q, product: %q)\n", appConfig.Name, appConfig.BinaryName, appConfig.ProductName)
	}
}

func syncPackageJSON(path string, appConfig Config) {
	if updateJSONFile(path, false, func(packageMap map[string]any) {
		packageMap["//"] = "DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json"
		packageMap["name"] = appConfig.Name
		packageMap["version"] = appConfig.Version
		packageMap["description"] = appConfig.Description
	}) {
		fmt.Printf("  -> Updated frontend/package.json (name: %q, version: %q)\n", appConfig.Name, appConfig.Version)
	}
}

func syncPackageLockJSON(path string, appConfig Config) {
	lockfileBytes, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	text := string(lockfileBytes)

	packagesIndex := strings.Index(text, `"packages":`)
	if packagesIndex == -1 {
		return
	}
	header := text[:packagesIndex]
	rest := text[packagesIndex:]

	nameRegex := regexp.MustCompile(`("name":\s*")[^"]*(")`)
	versionRegex := regexp.MustCompile(`("version":\s*")[^"]*(")`)

	header = nameRegex.ReplaceAllString(header, fmt.Sprintf("${1}%s${2}", appConfig.Name))
	header = versionRegex.ReplaceAllString(header, fmt.Sprintf("${1}%s${2}", appConfig.Version))

	rootPackageIndex := strings.Index(rest, `"":`)
	if rootPackageIndex != -1 {
		nextPackageIndex := strings.Index(rest, `"node_modules/`)
		if nextPackageIndex != -1 && nextPackageIndex > rootPackageIndex {
			rootPkgBlock := rest[rootPackageIndex:nextPackageIndex]
			updatedBlock := nameRegex.ReplaceAllString(rootPkgBlock, fmt.Sprintf("${1}%s${2}", appConfig.Name))
			updatedBlock = versionRegex.ReplaceAllString(updatedBlock, fmt.Sprintf("${1}%s${2}", appConfig.Version))
			rest = rest[:rootPackageIndex] + updatedBlock + rest[nextPackageIndex:]
		}
	}
	updated := header + rest

	changed, writeErr := writeFileIfChanged(path, []byte(updated), 0644)
	if writeErr != nil {
		return
	}
	if changed {
		fmt.Printf("  -> Updated frontend/package-lock.json (name: %q, version: %q)\n", appConfig.Name, appConfig.Version)
	}
}

func syncNodeModulesStamp(path string, appConfig Config) {
	stampBytes, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	text := string(stampBytes)

	nameRegex := regexp.MustCompile(`("name":\s*")[^"]*(")`)
	versionRegex := regexp.MustCompile(`("version":\s*")[^"]*(")`)

	packagesIndex := strings.Index(text, `"packages":`)
	var updated string
	if packagesIndex != -1 {
		header := text[:packagesIndex]
		rest := text[packagesIndex:]

		header = nameRegex.ReplaceAllString(header, fmt.Sprintf("${1}%s${2}", appConfig.Name))
		header = versionRegex.ReplaceAllString(header, fmt.Sprintf("${1}%s${2}", appConfig.Version))
		updated = header + rest
	} else {
		updated = text
	}

	_ = os.WriteFile(path, []byte(updated), 0644)
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

func syncIndexHTML(path string, appConfig Config) {
	indexHTMLBytes, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	// Replace title tag with comment banner and title
	titleRegex := regexp.MustCompile(`(<!--.*?-->\s*)?<title>.*?</title>`)
	newTitle := fmt.Sprintf("<!-- DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json -->\n    <title>%s — %s</title>", appConfig.ProductName, appConfig.Tagline)
	replaced := titleRegex.ReplaceAllString(string(indexHTMLBytes), newTitle)
	changed, writeErr := writeFileIfChanged(path, []byte(replaced), 0644)
	if writeErr != nil {
		return
	}
	if changed {
		fmt.Printf("  -> Updated frontend/index.html (<title>%s — %s</title>)\n", appConfig.ProductName, appConfig.Tagline)
	}
}

func generateGoVersion(path string, appConfig Config) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	userAgent := fmt.Sprintf("%s/%s", appConfig.ProductName, appConfig.Version)
	content := fmt.Sprintf(`// DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json
package version

const (
	Name        = %q
	ProductName = %q
	Tagline     = %q
	Version     = %q
	Author      = %q
	Description = %q
	BinaryName  = %q
	Repository  = %q
	UserAgent   = %q
)
`, appConfig.Name, appConfig.ProductName, appConfig.Tagline, appConfig.Version, appConfig.Author, appConfig.Description, appConfig.BinaryName, appConfig.Repository, userAgent)
	changed, writeErr := writeFileIfChanged(path, []byte(content), 0644)
	if writeErr != nil {
		return
	}
	if changed {
		fmt.Printf("  -> Generated internal/version/version.go\n")
	}
}

func generateTSVersion(path string, appConfig Config) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	content := fmt.Sprintf(`// DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json

export const APP_NAME = %q
export const APP_PRODUCT_NAME = %q
export const APP_TAGLINE = %q
export const APP_VERSION = %q
export const APP_AUTHOR = %q
export const APP_DESCRIPTION = %q
export const APP_BINARY_NAME = %q
export const APP_REPOSITORY = %q
`, appConfig.Name, appConfig.ProductName, appConfig.Tagline, appConfig.Version, appConfig.Author, appConfig.Description, appConfig.BinaryName, appConfig.Repository)
	changed, writeErr := writeFileIfChanged(path, []byte(content), 0644)
	if writeErr != nil {
		return
	}
	if changed {
		fmt.Printf("  -> Generated frontend/src/version.ts\n")
	}
}

func syncDesktopFile(path string, appConfig Config) {
	content := fmt.Sprintf(`# DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json
[Desktop Entry]
Type=Application
Name=%s
GenericName=%s
Comment=%s
Exec=%s %%u
Icon=%s
Terminal=false
Categories=Game;Utility;
StartupWMClass=%s
Keywords=elite;dangerous;expedition;astrometry;spansh;carrier;
`, appConfig.ProductName, appConfig.Tagline, appConfig.Description, appConfig.BinaryName, appConfig.BinaryName, appConfig.BinaryName)

	changed, writeErr := writeFileIfChanged(path, []byte(content), 0644)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", path, writeErr)
		return
	}
	if changed {
		fmt.Printf("  -> Generated %s\n", filepath.Base(path))
	}
}

func syncPKGBUILD(path string, appConfig Config) {
	pkgbuildBytes, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	banner := "# DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json\n"
	text := string(pkgbuildBytes)
	if !strings.HasPrefix(text, banner) {
		text = banner + text
	}

	pkgnameRegex := regexp.MustCompile(`(?m)^pkgname=.*$`)
	versionRegex := regexp.MustCompile(`(?m)^pkgver=.*$`)
	descRegex := regexp.MustCompile(`(?m)^pkgdesc=.*$`)
	urlRegex := regexp.MustCompile(`(?m)^url=.*$`)

	pkgName := appConfig.BinaryName + "-bin"

	updated := pkgnameRegex.ReplaceAll([]byte(text), []byte(fmt.Sprintf("pkgname=%s", pkgName)))
	updated = versionRegex.ReplaceAll(updated, []byte(fmt.Sprintf("pkgver=%s", appConfig.Version)))
	updated = descRegex.ReplaceAll(updated, []byte(fmt.Sprintf("pkgdesc=%q", appConfig.Description)))
	updated = urlRegex.ReplaceAll(updated, []byte(fmt.Sprintf("url=%q", appConfig.Repository)))

	changed, writeErr := writeFileIfChanged(path, updated, 0644)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", path, writeErr)
		return
	}
	if changed {
		fmt.Printf("  -> Updated %s (pkgname: %s, pkgver: %s, url: %s)\n", filepath.Base(path), pkgName, appConfig.Version, appConfig.Repository)
	}
}

func syncNFPM(path string, appConfig Config) {
	nfpmBytes, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	homepageRegex := regexp.MustCompile(`(?m)^homepage:\s*.*$`)
	updated := homepageRegex.ReplaceAll(nfpmBytes, []byte(fmt.Sprintf("homepage: %q", appConfig.Repository)))

	changed, writeErr := writeFileIfChanged(path, updated, 0644)
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", path, writeErr)
		return
	}
	if changed {
		fmt.Printf("  -> Updated %s (homepage: %s)\n", filepath.Base(path), appConfig.Repository)
	}
}

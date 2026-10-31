package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	Name        string `json:"name"`
	ProductName string `json:"productName"`
	Tagline     string `json:"tagline"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	BinaryName  string `json:"binaryName"`
}

func main() {
	rootDir, findError := findRootDir()
	if findError != nil {
		fmt.Fprintf(os.Stderr, "Error locating root directory: %v\n", findError)
		os.Exit(1)
	}

	configPath := filepath.Join(rootDir, "app.config.json")
	data, readError := os.ReadFile(configPath)
	if readError != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", configPath, readError)
		os.Exit(1)
	}

	var appConfig Config
	if parseError := json.Unmarshal(data, &appConfig); parseError != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", configPath, parseError)
		os.Exit(1)
	}

	if appConfig.ProductName == "" {
		appConfig.ProductName = "Planisphere"
	}
	if appConfig.BinaryName == "" {
		appConfig.BinaryName = "planisphere"
	}

	fmt.Printf("[sync-metadata] Reading app.config.json: %s v%s (%s)\n", appConfig.ProductName, appConfig.Version, appConfig.BinaryName)

	// 1. Sync wails.json
	syncWailsJSON(filepath.Join(rootDir, "wails.json"), appConfig)

	// 2. Sync frontend/package.json
	syncPackageJSON(filepath.Join(rootDir, "frontend", "package.json"), appConfig)

	// 3. Sync frontend/index.html
	syncIndexHTML(filepath.Join(rootDir, "frontend", "index.html"), appConfig)

	// 4. Generate internal/version/version.go (consumed directly by main.go & backend)
	generateGoVersion(filepath.Join(rootDir, "internal", "version", "version.go"), appConfig)

	// 5. Generate frontend/src/version.ts (consumed by frontend UI)
	generateTSVersion(filepath.Join(rootDir, "frontend", "src", "version.ts"), appConfig)

	// 6. Sync go.mod and internal Go imports
	syncGoMod(filepath.Join(rootDir, "go.mod"), rootDir, appConfig)

	fmt.Printf("✓ Metadata synchronized successfully across all project targets.\n")
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

func syncWailsJSON(path string, appConfig Config) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, readError)
		return
	}
	var manifestMap map[string]any
	if unmarshalError := json.Unmarshal(data, &manifestMap); unmarshalError != nil {
		return
	}
	manifestMap["_comment"] = "DO NOT EDIT DIRECTLY: Managed by scripts/sync_metadata.go from app.config.json"
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
	out, _ := json.MarshalIndent(manifestMap, "", "  ")
	_ = os.WriteFile(path, append(out, '\n'), 0644)
	fmt.Printf("  -> Updated wails.json (name: %q, binary: %q, product: %q)\n", appConfig.Name, appConfig.BinaryName, appConfig.ProductName)
}

func syncPackageJSON(path string, appConfig Config) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	var packageMap map[string]any
	if unmarshalError := json.Unmarshal(data, &packageMap); unmarshalError != nil {
		return
	}
	packageMap["//"] = "DO NOT EDIT METADATA DIRECTLY: Synced from app.config.json via 'npm run sync-metadata'"
	packageMap["name"] = appConfig.Name
	packageMap["version"] = appConfig.Version
	packageMap["description"] = appConfig.Description
	out, _ := json.MarshalIndent(packageMap, "", "  ")
	_ = os.WriteFile(path, append(out, '\n'), 0644)
	fmt.Printf("  -> Updated frontend/package.json (name: %q, version: %q)\n", appConfig.Name, appConfig.Version)
}

func syncIndexHTML(path string, appConfig Config) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		return
	}
	// Replace title tag with comment banner and title
	titleRegex := regexp.MustCompile(`(<!--.*?-->\s*)?<title>.*?</title>`)
	newTitle := fmt.Sprintf("<!-- Managed by scripts/sync_metadata.go. Edit app.config.json instead. -->\n    <title>%s — %s</title>", appConfig.ProductName, appConfig.Tagline)
	replaced := titleRegex.ReplaceAllString(string(data), newTitle)
	_ = os.WriteFile(path, []byte(replaced), 0644)
	fmt.Printf("  -> Updated frontend/index.html (<title>%s — %s</title>)\n", appConfig.ProductName, appConfig.Tagline)
}

func generateGoVersion(path string, appConfig Config) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	userAgent := fmt.Sprintf("%s/%s", appConfig.ProductName, appConfig.Version)
	content := fmt.Sprintf(`// Code generated by scripts/sync_metadata.go; DO NOT EDIT.
// Source: app.config.json
package version

const (
	Name        = %q
	ProductName = %q
	Tagline     = %q
	Version     = %q
	Author      = %q
	Description = %q
	BinaryName  = %q
	UserAgent   = %q
)
`, appConfig.Name, appConfig.ProductName, appConfig.Tagline, appConfig.Version, appConfig.Author, appConfig.Description, appConfig.BinaryName, userAgent)
	_ = os.WriteFile(path, []byte(content), 0644)
	fmt.Printf("  -> Generated internal/version/version.go\n")
}

func generateTSVersion(path string, appConfig Config) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	content := fmt.Sprintf(`// Code generated by scripts/sync_metadata.go; DO NOT EDIT.
// Source: app.config.json

export const APP_NAME = %q
export const APP_PRODUCT_NAME = %q
export const APP_TAGLINE = %q
export const APP_VERSION = %q
export const APP_AUTHOR = %q
export const APP_DESCRIPTION = %q
export const APP_BINARY_NAME = %q
`, appConfig.Name, appConfig.ProductName, appConfig.Tagline, appConfig.Version, appConfig.Author, appConfig.Description, appConfig.BinaryName)
	_ = os.WriteFile(path, []byte(content), 0644)
	fmt.Printf("  -> Generated frontend/src/version.ts\n")
}

func syncGoMod(path, rootDir string, appConfig Config) {
	data, readError := os.ReadFile(path)
	if readError != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, readError)
		return
	}
	moduleRegex := regexp.MustCompile(`(?m)^module\s+([^\s\r\n]+)`)
	matches := moduleRegex.FindSubmatch(data)
	if len(matches) < 2 {
		return
	}
	oldModule := string(matches[1])
	if oldModule != appConfig.Name {
		updated := moduleRegex.ReplaceAll(data, []byte("module "+appConfig.Name))
		if writeError := os.WriteFile(path, updated, 0644); writeError != nil {
			fmt.Fprintf(os.Stderr, "Error updating %s: %v\n", path, writeError)
			return
		}
		fmt.Printf("  -> Updated go.mod (module: %q -> %q)\n", oldModule, appConfig.Name)
		updateGoImports(rootDir, oldModule, appConfig.Name)
	}
}

func updateGoImports(rootDir, oldModule, newModule string) {
	oldPattern := []byte(`"` + oldModule + `/`)
	newPattern := []byte(`"` + newModule + `/`)
	count := 0

	_ = filepath.Walk(rootDir, func(path string, info os.FileInfo, walkError error) error {
		if walkError != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") {
			return nil
		}
		content, readError := os.ReadFile(path)
		if readError != nil {
			return nil
		}
		if bytes.Contains(content, oldPattern) {
			newContent := bytes.ReplaceAll(content, oldPattern, newPattern)
			if writeError := os.WriteFile(path, newContent, info.Mode().Perm()); writeError == nil {
				count++
			}
		}
		return nil
	})
	if count > 0 {
		fmt.Printf("  -> Updated Go import paths in %d files (%s -> %s)\n", count, oldModule, newModule)
	}
}

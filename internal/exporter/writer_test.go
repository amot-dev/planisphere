package exporter_test

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"planisphere/internal/exporter"
)

func TestWriteExportSingleFileAndBase64DataURI(testingContext *testing.T) {
	temporaryDir := testingContext.TempDir()

	htmlPath := filepath.Join(temporaryDir, "nested", "report.html")
	writtenHTMLPath, htmlError := exporter.WriteExport(htmlPath, []exporter.BundleFile{
		{Name: "report.html", ContentText: "<html><body>Planisphere</body></html>"},
	})
	if htmlError != nil {
		testingContext.Fatalf("write single html file: %v", htmlError)
	}
	if writtenHTMLPath != htmlPath {
		testingContext.Fatalf("expected written path %q, got %q", htmlPath, writtenHTMLPath)
	}
	savedHTML, readError := os.ReadFile(htmlPath)
	if readError != nil || string(savedHTML) != "<html><body>Planisphere</body></html>" {
		testingContext.Fatalf("unexpected saved html: %q, err=%v", string(savedHTML), readError)
	}

	rawPNGHeader := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	dataURIPayload := "data:image/png;base64," + base64.StdEncoding.EncodeToString(rawPNGHeader)
	pngPath := filepath.Join(temporaryDir, "summary.png")
	if _, pngError := exporter.WriteExport(pngPath, []exporter.BundleFile{
		{Name: "summary.png", ContentBase64: dataURIPayload},
	}); pngError != nil {
		testingContext.Fatalf("write single base64 png: %v", pngError)
	}
	savedPNG, readPNGError := os.ReadFile(pngPath)
	if readPNGError != nil || string(savedPNG) != string(rawPNGHeader) {
		testingContext.Fatalf("unexpected saved png bytes: %v, err=%v", savedPNG, readPNGError)
	}
}

type zipArchiveEntries struct {
	directoryCounts map[string]int
	fileContents    map[string]string
	methods         map[string]uint16
}

func readZipEntries(testingContext *testing.T, zipPath string) zipArchiveEntries {
	testingContext.Helper()
	archiveReader, openZipError := zip.OpenReader(zipPath)
	if openZipError != nil {
		testingContext.Fatalf("open created zip: %v", openZipError)
	}
	defer archiveReader.Close()

	snapshot := zipArchiveEntries{
		directoryCounts: make(map[string]int),
		fileContents:    make(map[string]string),
		methods:         make(map[string]uint16, len(archiveReader.File)),
	}
	for _, zipEntry := range archiveReader.File {
		if zipEntry.Modified.Year() < 2020 {
			testingContext.Fatalf("expected recent timestamp on zip entry %q, got %v", zipEntry.Name, zipEntry.Modified)
		}
		snapshot.methods[zipEntry.Name] = zipEntry.Method
		if strings.HasSuffix(zipEntry.Name, "/") {
			snapshot.directoryCounts[zipEntry.Name]++
			continue
		}
		entryReader, entryOpenError := zipEntry.Open()
		if entryOpenError != nil {
			testingContext.Fatalf("open zip entry %q: %v", zipEntry.Name, entryOpenError)
		}
		entryBytes, readEntryError := io.ReadAll(entryReader)
		if closeEntryError := entryReader.Close(); closeEntryError != nil {
			testingContext.Fatalf("close zip entry %q: %v", zipEntry.Name, closeEntryError)
		}
		if readEntryError != nil {
			testingContext.Fatalf("read zip entry %q: %v", zipEntry.Name, readEntryError)
		}
		snapshot.fileContents[zipEntry.Name] = string(entryBytes)
	}
	return snapshot
}

func TestWriteExportZipBundleSanitizationAndDeduplication(testingContext *testing.T) {
	temporaryDir := testingContext.TempDir()
	zipPath := filepath.Join(temporaryDir, "expedition_exporter.zip")

	files := []exporter.BundleFile{
		{Name: "../outside/01-summary.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte("png-one"))},
		{Name: "outside/01-summary.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte("png-two"))},
		{Name: "outside/02-upper.PNG", ContentBase64: base64.StdEncoding.EncodeToString([]byte("png-upper"))},
		{Name: `folder\bad:name?.html`, ContentText: "<h1>Report</h1>"},
		{Name: "data/manifest.json", ContentText: `{"name":"planisphere"}`},
	}
	writtenZipPath, zipError := exporter.WriteExport(zipPath, files)
	if zipError != nil {
		testingContext.Fatalf("write zip bundle: %v", zipError)
	}
	if writtenZipPath != zipPath {
		testingContext.Fatalf("expected zip path %q, got %q", zipPath, writtenZipPath)
	}

	archiveSnapshot := readZipEntries(testingContext, zipPath)

	if archiveSnapshot.directoryCounts["outside/"] != 1 || archiveSnapshot.directoryCounts["folder/"] != 1 || archiveSnapshot.directoryCounts["data/"] != 1 {
		testingContext.Fatalf("expected explicit directory entries outside/, folder/, and data/, got %v", archiveSnapshot.directoryCounts)
	}
	if archiveSnapshot.fileContents["outside/01-summary.png"] != "png-one" ||
		archiveSnapshot.fileContents["outside/01-summary-2.png"] != "png-two" ||
		archiveSnapshot.fileContents["outside/02-upper.PNG"] != "png-upper" ||
		archiveSnapshot.fileContents["folder/bad_name_.html"] != "<h1>Report</h1>" ||
		archiveSnapshot.fileContents["data/manifest.json"] != `{"name":"planisphere"}` {
		testingContext.Fatalf("unexpected zip entry contents: %v", archiveSnapshot.fileContents)
	}

	expectedMethods := map[string]uint16{
		"outside/":                 zip.Store,
		"outside/01-summary.png":   zip.Store,
		"outside/01-summary-2.png": zip.Store,
		"outside/02-upper.PNG":     zip.Store,
		"folder/":                  zip.Store,
		"folder/bad_name_.html":    zip.Deflate,
		"data/":                    zip.Store,
		"data/manifest.json":       zip.Deflate,
	}
	if len(archiveSnapshot.methods) != len(expectedMethods) {
		testingContext.Fatalf("expected %d zip entries, got %d (%v)", len(expectedMethods), len(archiveSnapshot.methods), archiveSnapshot.methods)
	}
	for entryName, expectedMethod := range expectedMethods {
		if observedMethod, exists := archiveSnapshot.methods[entryName]; !exists || observedMethod != expectedMethod {
			testingContext.Fatalf("zip entry %q method = (%d, %v), want (%d, true)", entryName, observedMethod, exists, expectedMethod)
		}
	}
}

func TestWriteExportRejectsInvalidInputs(testingContext *testing.T) {
	temporaryDir := testingContext.TempDir()
	if _, emptyError := exporter.WriteExport(filepath.Join(temporaryDir, "empty.zip"), nil); emptyError == nil {
		testingContext.Fatalf("expected error for empty file list")
	}
	if _, badBase64Error := exporter.WriteExport(filepath.Join(temporaryDir, "bad.png"), []exporter.BundleFile{
		{Name: "bad.png", ContentBase64: "%%%not-valid-base64%%%"},
	}); badBase64Error == nil {
		testingContext.Fatalf("expected error for malformed base64 payload")
	}
	truncatedPNGPath := filepath.Join(temporaryDir, "truncated.png")
	if _, emptyDataURIError := exporter.WriteExport(truncatedPNGPath, []exporter.BundleFile{
		{Name: "truncated.png", ContentBase64: "data:,"},
	}); emptyDataURIError == nil {
		testingContext.Fatalf("expected error for empty data:, base64 payload")
	}
	if _, statError := os.Stat(truncatedPNGPath); !errors.Is(statError, os.ErrNotExist) {
		testingContext.Fatalf("expected no output file %q after empty base64 failure, stat error=%v", truncatedPNGPath, statError)
	}
	if _, emptyZipDataURIError := exporter.WriteExport(filepath.Join(temporaryDir, "truncated.zip"), []exporter.BundleFile{
		{Name: "images/01-summary.png", ContentBase64: "data:,"},
	}); emptyZipDataURIError == nil {
		testingContext.Fatalf("expected error for empty data:, base64 payload inside zip")
	}
}

func TestWriteExportNormalizesMixedCaseDirectoryHeaders(testingContext *testing.T) {
	temporaryDir := testingContext.TempDir()
	zipPath := filepath.Join(temporaryDir, "mixed_case_dirs.zip")

	files := []exporter.BundleFile{
		{Name: "Images/01-summary.png", ContentText: "summary"},
		{Name: "images/02-details.png", ContentText: "details"},
		{Name: "Images/Sub/a.png", ContentText: "nested-a"},
		{Name: "images/sub/b.png", ContentText: "nested-b"},
		{Name: "report", ContentText: "report-first"},
		{Name: "report-2/01.png", ContentText: "nested-report-image"},
		{Name: "report", ContentText: "report-second"},
	}

	if _, writeError := exporter.WriteExport(zipPath, files); writeError != nil {
		testingContext.Fatalf("write zip bundle with mixed-case directories: %v", writeError)
	}

	archiveSnapshot := readZipEntries(testingContext, zipPath)
	directoryCounts := archiveSnapshot.directoryCounts
	fileContents := archiveSnapshot.fileContents

	expectedDirectories := map[string]int{
		"Images/":     1,
		"Images/Sub/": 1,
		"report-2/":   1,
	}
	if len(directoryCounts) != len(expectedDirectories) {
		testingContext.Fatalf("expected directory headers %v, got %v", expectedDirectories, directoryCounts)
	}
	for directoryName, expectedCount := range expectedDirectories {
		if directoryCounts[directoryName] != expectedCount {
			testingContext.Fatalf("expected directory %q count %d, got %v", directoryName, expectedCount, directoryCounts)
		}
	}

	expectedFiles := map[string]string{
		"Images/01-summary.png": "summary",
		"Images/02-details.png": "details",
		"Images/Sub/a.png":      "nested-a",
		"Images/Sub/b.png":      "nested-b",
		"report":                "report-first",
		"report-2/01.png":       "nested-report-image",
		"report-3":              "report-second",
	}
	if len(fileContents) != len(expectedFiles) {
		testingContext.Fatalf("expected files %v, got %v", expectedFiles, fileContents)
	}
	for entryName, expectedBody := range expectedFiles {
		if fileContents[entryName] != expectedBody {
			testingContext.Fatalf("expected entry %q to contain %q, got %q (all=%v)", entryName, expectedBody, fileContents[entryName], fileContents)
		}
	}
}

func TestWriteExportRejectsFileAndDirectoryCollisions(testingContext *testing.T) {
	collisionCases := []struct {
		name  string
		files []exporter.BundleFile
	}{
		{
			name: "mixed-case directory before file",
			files: []exporter.BundleFile{
				{Name: "Images/01.png", ContentText: "png"},
				{Name: "images", ContentText: "not a folder"},
			},
		},
		{
			name: "mixed-case file before directory",
			files: []exporter.BundleFile{
				{Name: "Images", ContentText: "not a folder"},
				{Name: "images/01.png", ContentText: "png"},
			},
		},
		{
			name: "nested mixed-case directory before file",
			files: []exporter.BundleFile{
				{Name: "Images/Sub/01.png", ContentText: "png"},
				{Name: "images/sub", ContentText: "not a folder"},
			},
		},
	}

	for _, testCase := range collisionCases {
		testingContext.Run(testCase.name, func(subTest *testing.T) {
			outputDirectory := subTest.TempDir()
			zipPath := filepath.Join(outputDirectory, "collision.zip")

			if _, writeError := exporter.WriteExport(zipPath, testCase.files); writeError == nil {
				subTest.Fatalf("expected collision error for %s, got nil", testCase.name)
			}

			if _, statError := os.Stat(zipPath); !errors.Is(statError, os.ErrNotExist) {
				subTest.Fatalf("expected no output file %q after collision failure, stat error=%v", zipPath, statError)
			}

			remainingEntries, readDirectoryError := os.ReadDir(outputDirectory)
			if readDirectoryError != nil {
				subTest.Fatalf("read temp dir after failed export: %v", readDirectoryError)
			}
			if len(remainingEntries) != 0 {
				subTest.Fatalf("expected no leftover files in output directory, found %d", len(remainingEntries))
			}
		})
	}
}


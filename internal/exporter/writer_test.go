package exporter_test

import (
	"archive/zip"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
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

func TestWriteExportZipBundleSanitizationAndDeduplication(testingContext *testing.T) {
	temporaryDir := testingContext.TempDir()
	zipPath := filepath.Join(temporaryDir, "expedition_exporter.zip")

	files := []exporter.BundleFile{
		{Name: "../outside/01-summary.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte("png-one"))},
		{Name: "outside/01-summary.png", ContentBase64: base64.StdEncoding.EncodeToString([]byte("png-two"))},
		{Name: `folder\bad:name?.html`, ContentText: "<h1>Report</h1>"},
	}
	writtenZipPath, zipError := exporter.WriteExport(zipPath, files)
	if zipError != nil {
		testingContext.Fatalf("write zip bundle: %v", zipError)
	}
	if writtenZipPath != zipPath {
		testingContext.Fatalf("expected zip path %q, got %q", zipPath, writtenZipPath)
	}

	archiveReader, openZipError := zip.OpenReader(zipPath)
	if openZipError != nil {
		testingContext.Fatalf("open created zip: %v", openZipError)
	}
	defer archiveReader.Close()

	entries := make(map[string]string, len(archiveReader.File))
	for _, zipEntry := range archiveReader.File {
		entryReader, entryOpenError := zipEntry.Open()
		if entryOpenError != nil {
			testingContext.Fatalf("open zip entry %q: %v", zipEntry.Name, entryOpenError)
		}
		entryBytes, readEntryError := io.ReadAll(entryReader)
		entryReader.Close()
		if readEntryError != nil {
			testingContext.Fatalf("read zip entry %q: %v", zipEntry.Name, readEntryError)
		}
		entries[zipEntry.Name] = string(entryBytes)
	}

	if entries["outside/01-summary.png"] != "png-one" {
		testingContext.Fatalf("expected sanitized first entry, got entries=%v", entries)
	}
	if entries["outside/01-summary-2.png"] != "png-two" {
		testingContext.Fatalf("expected deduplicated second entry, got entries=%v", entries)
	}
	if entries["folder/bad_name_.html"] != "<h1>Report</h1>" {
		testingContext.Fatalf("expected sanitized html entry, got entries=%v", entries)
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
}


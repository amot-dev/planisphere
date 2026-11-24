package exporter

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// WriteExport writes a single file directly or packages multiple files into a
// zip archive at targetPath using atomic temporary-file replacement.
func WriteExport(targetPath string, files []BundleFile) (string, error) {
	cleanedTargetPath := filepath.Clean(strings.TrimSpace(targetPath))
	if cleanedTargetPath == "" || cleanedTargetPath == "." {
		return "", errors.New("export target path is required")
	}
	if len(files) == 0 {
		return "", errors.New("at least one export file is required")
	}

	parentDirectory := filepath.Dir(cleanedTargetPath)
	if mkdirError := os.MkdirAll(parentDirectory, 0o755); mkdirError != nil {
		return "", fmt.Errorf("create export directory %q: %w", parentDirectory, mkdirError)
	}

	writeAsZip := len(files) > 1 || strings.EqualFold(filepath.Ext(cleanedTargetPath), ".zip")
	if !writeAsZip {
		fileBytes, decodeError := decodeBundleFile(files[0])
		if decodeError != nil {
			return "", decodeError
		}
		if writeError := writeBytesAtomically(cleanedTargetPath, fileBytes); writeError != nil {
			return "", writeError
		}
		return cleanedTargetPath, nil
	}

	if zipError := writeZipBundleAtomically(cleanedTargetPath, files); zipError != nil {
		return "", zipError
	}
	return cleanedTargetPath, nil
}

// SanitizeArchiveEntryName cleans a relative export path for safe cross-platform
// file and zip entry creation, rejecting traversal segments and invalid characters.
func SanitizeArchiveEntryName(rawName string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(rawName), "\\", "/")
	rawSegments := strings.Split(normalized, "/")
	safeSegments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		cleanedSegment := sanitizePathSegment(segment)
		if cleanedSegment != "" {
			safeSegments = append(safeSegments, cleanedSegment)
		}
	}
	if len(safeSegments) == 0 {
		return "export-file.bin"
	}
	return path.Join(safeSegments...)
}

func sanitizePathSegment(rawSegment string) string {
	trimmed := strings.TrimSpace(rawSegment)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(trimmed))
	for _, character := range trimmed {
		if character < 0x20 || strings.ContainsRune(`<>:"|?*`, character) {
			builder.WriteByte('_')
			continue
		}
		builder.WriteRune(character)
	}
	sanitized := strings.Trim(builder.String(), ". ")
	if sanitized == "" {
		return ""
	}
	return sanitized
}

func decodeBundleFile(bundleFile BundleFile) ([]byte, error) {
	if bundleFile.ContentBase64 != "" {
		encodedPayload := bundleFile.ContentBase64
		if commaIndex := strings.Index(encodedPayload, ","); strings.HasPrefix(encodedPayload, "data:") && commaIndex >= 0 {
			encodedPayload = encodedPayload[commaIndex+1:]
		}
		decodedBytes, decodeError := base64.StdEncoding.DecodeString(encodedPayload)
		if decodeError != nil {
			return nil, fmt.Errorf("decode base64 content for %q: %w", bundleFile.Name, decodeError)
		}
		if len(decodedBytes) == 0 {
			return nil, fmt.Errorf("decoded base64 content for %q is empty", bundleFile.Name)
		}
		return decodedBytes, nil
	}
	return []byte(bundleFile.ContentText), nil
}

func deduplicateEntryName(
	candidateName string,
	usedNames map[string]int,
	canonicalDirectories map[string]string,
) string {
	normalizedKey := strings.ToLower(candidateName)
	seenCount := usedNames[normalizedKey]
	if seenCount == 0 {
		usedNames[normalizedKey] = 1
		return candidateName
	}
	extension := path.Ext(candidateName)
	baseWithoutExtension := strings.TrimSuffix(candidateName, extension)
	for suffix := seenCount + 1; ; suffix++ {
		nextCandidate := fmt.Sprintf("%s-%d%s", baseWithoutExtension, suffix, extension)
		nextKey := strings.ToLower(nextCandidate)
		if _, collidesWithDirectory := canonicalDirectories[nextKey]; !collidesWithDirectory && usedNames[nextKey] == 0 {
			usedNames[normalizedKey] = suffix
			usedNames[nextKey] = 1
			return nextCandidate
		}
	}
}

func writeViaTempFile(targetPath, tempPattern string, writeContent func(tempFile *os.File) error) error {
	tempFile, createError := os.CreateTemp(filepath.Dir(targetPath), tempPattern)
	if createError != nil {
		return fmt.Errorf("create temporary export file: %w", createError)
	}
	tempPath := tempFile.Name()
	committed := false
	defer func() {
		if !committed {
			os.Remove(tempPath)
		}
	}()

	if writeError := writeContent(tempFile); writeError != nil {
		return errors.Join(writeError, tempFile.Close())
	}
	if closeError := tempFile.Close(); closeError != nil {
		return fmt.Errorf("close export file %q: %w", targetPath, closeError)
	}
	if renameError := os.Rename(tempPath, targetPath); renameError != nil {
		return fmt.Errorf("finalize export file %q: %w", targetPath, renameError)
	}
	committed = true
	return nil
}

func writeBytesAtomically(targetPath string, contentBytes []byte) error {
	return writeViaTempFile(targetPath, ".planisphere-export-*", func(tempFile *os.File) error {
		if _, writeError := tempFile.Write(contentBytes); writeError != nil {
			return fmt.Errorf("write export file %q: %w", targetPath, writeError)
		}
		return nil
	})
}

func writeZipBundleAtomically(targetPath string, files []BundleFile) error {
	return writeViaTempFile(targetPath, ".planisphere-export-*.zip", func(tempFile *os.File) error {
		archiveWriter := zip.NewWriter(tempFile)
		if writeEntriesError := writeZipEntries(archiveWriter, files); writeEntriesError != nil {
			return errors.Join(writeEntriesError, archiveWriter.Close())
		}
		if closeZipError := archiveWriter.Close(); closeZipError != nil {
			return fmt.Errorf("finalize zip archive %q: %w", targetPath, closeZipError)
		}
		return nil
	})
}

// writeZipEntries populates the archive via CreateHeader rather than Create so
// every entry records a valid UTC Modified timestamp, explicit Unix permissions,
// and zip.Store bypass for pre-compressed PNG images.
func writeZipEntries(archiveWriter *zip.Writer, files []BundleFile) error {
	exportTimestamp := time.Now().UTC()
	usedNames := make(map[string]int, len(files))
	canonicalDirectories := make(map[string]string)
	for _, bundleFile := range files {
		fileBytes, decodeError := decodeBundleFile(bundleFile)
		if decodeError != nil {
			return decodeError
		}
		sanitizedName := SanitizeArchiveEntryName(bundleFile.Name)
		canonicalName, pathError := canonicalizeZipEntryPath(sanitizedName, usedNames, canonicalDirectories)
		if pathError != nil {
			return pathError
		}
		entryName := deduplicateEntryName(canonicalName, usedNames, canonicalDirectories)
		if directoryError := ensureZipDirectoryHeaders(archiveWriter, entryName, canonicalDirectories, exportTimestamp); directoryError != nil {
			return directoryError
		}
		compressionMethod := zip.Deflate
		if strings.EqualFold(path.Ext(entryName), ".png") {
			compressionMethod = zip.Store
		}
		fileHeader := &zip.FileHeader{
			Name:     entryName,
			Method:   compressionMethod,
			Modified: exportTimestamp,
		}
		fileHeader.SetMode(0o644)
		entryWriter, entryError := archiveWriter.CreateHeader(fileHeader)
		if entryError != nil {
			return fmt.Errorf("create zip entry %q: %w", entryName, entryError)
		}
		if _, writeError := entryWriter.Write(fileBytes); writeError != nil {
			return fmt.Errorf("write zip entry %q: %w", entryName, writeError)
		}
	}
	return nil
}

func canonicalizeZipEntryPath(
	sanitizedName string,
	usedNames map[string]int,
	canonicalDirectories map[string]string,
) (string, error) {
	if existingDirectory, collidesWithDirectory := canonicalDirectories[strings.ToLower(sanitizedName)]; collidesWithDirectory {
		return "", fmt.Errorf("zip entry %q collides with existing directory %q", sanitizedName, existingDirectory+"/")
	}
	directoryPath := path.Dir(sanitizedName)
	if directoryPath == "." || directoryPath == "/" {
		return sanitizedName, nil
	}
	if existingCanonical, exists := canonicalDirectories[strings.ToLower(directoryPath)]; exists {
		return existingCanonical + "/" + path.Base(sanitizedName), nil
	}
	canonicalPrefix := ""
	segmentStart := 0
	for segmentStart <= len(directoryPath) {
		var segment string
		segment, segmentStart = nextPathSegment(directoryPath, segmentStart)
		candidatePrefix := path.Join(canonicalPrefix, segment)
		prefixKey := strings.ToLower(candidatePrefix)
		if usedNames[prefixKey] > 0 {
			return "", fmt.Errorf("zip directory %q collides with existing file entry", candidatePrefix+"/")
		}
		if existingCanonical, exists := canonicalDirectories[prefixKey]; exists {
			canonicalPrefix = existingCanonical
		} else {
			canonicalPrefix = candidatePrefix
		}
	}
	return canonicalPrefix + "/" + path.Base(sanitizedName), nil
}

// ensureZipDirectoryHeaders emits explicit os.ModeDir headers via CreateHeader
// so archive extractors see canonical directory entries with UTC Modified timestamps.
func ensureZipDirectoryHeaders(
	archiveWriter *zip.Writer,
	entryName string,
	canonicalDirectories map[string]string,
	exportTimestamp time.Time,
) error {
	directoryPath := path.Dir(entryName)
	if directoryPath == "." || directoryPath == "/" {
		return nil
	}
	if _, alreadyCreated := canonicalDirectories[strings.ToLower(directoryPath)]; alreadyCreated {
		return nil
	}
	canonicalPrefix := ""
	segmentStart := 0
	for segmentStart <= len(directoryPath) {
		var segment string
		segment, segmentStart = nextPathSegment(directoryPath, segmentStart)
		canonicalPrefix = path.Join(canonicalPrefix, segment)
		directoryKey := strings.ToLower(canonicalPrefix)
		if existingCanonical, alreadyCreated := canonicalDirectories[directoryKey]; alreadyCreated {
			canonicalPrefix = existingCanonical
			continue
		}
		directoryEntryName := canonicalPrefix + "/"
		directoryHeader := &zip.FileHeader{
			Name:     directoryEntryName,
			Method:   zip.Store,
			Modified: exportTimestamp,
		}
		directoryHeader.SetMode(0o755 | os.ModeDir)
		if _, headerError := archiveWriter.CreateHeader(directoryHeader); headerError != nil {
			return fmt.Errorf("create zip directory %q: %w", directoryEntryName, headerError)
		}
		canonicalDirectories[directoryKey] = canonicalPrefix
	}
	return nil
}

func nextPathSegment(directoryPath string, segmentStart int) (string, int) {
	slashOffset := strings.IndexByte(directoryPath[segmentStart:], '/')
	if slashOffset < 0 {
		return directoryPath[segmentStart:], len(directoryPath) + 1
	}
	return directoryPath[segmentStart : segmentStart+slashOffset], segmentStart + slashOffset + 1
}

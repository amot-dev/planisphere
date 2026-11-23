package export

import (
	"archive/zip"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
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
		return decodedBytes, nil
	}
	return []byte(bundleFile.ContentText), nil
}

func deduplicateEntryName(candidateName string, usedNames map[string]int) string {
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
		if usedNames[nextKey] == 0 {
			usedNames[normalizedKey] = suffix
			usedNames[nextKey] = 1
			return nextCandidate
		}
	}
}

func writeBytesAtomically(targetPath string, contentBytes []byte) error {
	tempFile, createError := os.CreateTemp(filepath.Dir(targetPath), ".planisphere-export-*")
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

	if _, writeError := tempFile.Write(contentBytes); writeError != nil {
		closeError := tempFile.Close()
		return errors.Join(fmt.Errorf("write export file %q: %w", targetPath, writeError), closeError)
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

func writeZipBundleAtomically(targetPath string, files []BundleFile) error {
	tempFile, createError := os.CreateTemp(filepath.Dir(targetPath), ".planisphere-export-*.zip")
	if createError != nil {
		return fmt.Errorf("create temporary zip archive: %w", createError)
	}
	tempPath := tempFile.Name()
	committed := false
	defer func() {
		if !committed {
			os.Remove(tempPath)
		}
	}()

	archiveWriter := zip.NewWriter(tempFile)
	if writeEntriesError := writeZipEntries(archiveWriter, files); writeEntriesError != nil {
		return errors.Join(writeEntriesError, archiveWriter.Close(), tempFile.Close())
	}
	if closeZipError := archiveWriter.Close(); closeZipError != nil {
		return errors.Join(fmt.Errorf("finalize zip archive %q: %w", targetPath, closeZipError), tempFile.Close())
	}
	if closeFileError := tempFile.Close(); closeFileError != nil {
		return fmt.Errorf("close zip archive %q: %w", targetPath, closeFileError)
	}
	if renameError := os.Rename(tempPath, targetPath); renameError != nil {
		return fmt.Errorf("move zip archive to %q: %w", targetPath, renameError)
	}
	committed = true
	return nil
}

func writeZipEntries(archiveWriter *zip.Writer, files []BundleFile) error {
	usedNames := make(map[string]int, len(files))
	for _, bundleFile := range files {
		fileBytes, decodeError := decodeBundleFile(bundleFile)
		if decodeError != nil {
			return decodeError
		}
		entryName := deduplicateEntryName(SanitizeArchiveEntryName(bundleFile.Name), usedNames)
		entryWriter, entryError := archiveWriter.Create(entryName)
		if entryError != nil {
			return fmt.Errorf("create zip entry %q: %w", entryName, entryError)
		}
		if _, writeError := entryWriter.Write(fileBytes); writeError != nil {
			return fmt.Errorf("write zip entry %q: %w", entryName, writeError)
		}
	}
	return nil
}

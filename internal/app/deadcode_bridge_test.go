package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	wailsBridgeClausePattern = regexp.MustCompile(`(?s)(?:export|import)\s*\{[^}]*\}\s*from\s*['"][^'"]*wailsjs/go/app/App['"]`)
	lineCommentPattern       = regexp.MustCompile(`(?m)^\s*//.*$`)
	blockCommentPattern      = regexp.MustCompile(`(?ms)^\s*/\*.*?\*/`)
	identifierTokenPattern   = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)
)

func TestExportedAppMethodsUsedByFrontend(testingContext *testing.T) {
	exportedMethodLocations := collectExportedAppMethods(testingContext, ".")
	if len(exportedMethodLocations) == 0 {
		testingContext.Fatal("expected at least one exported App method in internal/app")
	}

	frontendSourceRoot := filepath.Join("..", "..", "frontend", "src")
	frontendIdentifiers := collectFrontendProductionIdentifiers(testingContext, frontendSourceRoot)

	var unusedMethods []string
	for methodName, sourceLocation := range exportedMethodLocations {
		if _, found := frontendIdentifiers[methodName]; !found {
			unusedMethods = append(unusedMethods, methodName+" ("+sourceLocation+")")
		}
	}
	sort.Strings(unusedMethods)
	if len(unusedMethods) > 0 {
		testingContext.Fatalf("exported *App Wails RPC methods unused by production frontend code:\n  %s", strings.Join(unusedMethods, "\n  "))
	}
}

func collectExportedAppMethods(testingContext *testing.T, appDirectory string) map[string]string {
	testingContext.Helper()
	directoryEntries, readDirError := os.ReadDir(appDirectory)
	if readDirError != nil {
		testingContext.Fatalf("read internal/app directory: %v", readDirError)
	}

	fileSet := token.NewFileSet()
	methodsByName := make(map[string]string)
	for _, entry := range directoryEntries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".go") || strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		parsedFile, parseError := parser.ParseFile(fileSet, filepath.Join(appDirectory, fileName), nil, 0)
		if parseError != nil {
			testingContext.Fatalf("parse %s: %v", fileName, parseError)
		}
		for _, declaration := range parsedFile.Decls {
			functionDecl, isFunc := declaration.(*ast.FuncDecl)
			if !isFunc || !functionDecl.Name.IsExported() || !isAppReceiver(functionDecl.Recv) {
				continue
			}
			methodsByName[functionDecl.Name.Name] = fileName
		}
	}
	return methodsByName
}

func isAppReceiver(receiverList *ast.FieldList) bool {
	if receiverList == nil || len(receiverList.List) == 0 {
		return false
	}
	receiverExpr := receiverList.List[0].Type
	if starExpr, isPointer := receiverExpr.(*ast.StarExpr); isPointer {
		receiverExpr = starExpr.X
	}
	receiverIdent, isIdent := receiverExpr.(*ast.Ident)
	return isIdent && receiverIdent.Name == "App"
}

func collectFrontendProductionIdentifiers(testingContext *testing.T, frontendSourceRoot string) map[string]struct{} {
	testingContext.Helper()
	identifiers := make(map[string]struct{}, 2048)
	walkError := filepath.WalkDir(frontendSourceRoot, func(currentPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relativePath, relError := filepath.Rel(frontendSourceRoot, currentPath)
		if relError != nil {
			return relError
		}
		relativeSlashPath := filepath.ToSlash(relativePath)
		if entry.IsDir() {
			if relativeSlashPath == "test" || relativeSlashPath == "profile" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isProductionFrontendFile(entry.Name()) {
			return nil
		}
		rawBytes, readError := os.ReadFile(currentPath)
		if readError != nil {
			return readError
		}
		sanitizedSource := sanitizeFrontendSource(string(rawBytes), relativeSlashPath == "api/backend.ts")
		for _, tokenMatch := range identifierTokenPattern.FindAllString(sanitizedSource, -1) {
			identifiers[tokenMatch] = struct{}{}
		}
		return nil
	})
	if walkError != nil {
		testingContext.Fatalf("walk frontend production sources: %v", walkError)
	}
	return identifiers
}

func isProductionFrontendFile(fileName string) bool {
	if !strings.HasSuffix(fileName, ".ts") && !strings.HasSuffix(fileName, ".tsx") {
		return false
	}
	return !strings.HasSuffix(fileName, ".test.ts") &&
		!strings.HasSuffix(fileName, ".test.tsx") &&
		!strings.HasSuffix(fileName, ".d.ts")
}

func sanitizeFrontendSource(sourceText string, isBackendFacade bool) string {
	if isBackendFacade {
		sourceText = wailsBridgeClausePattern.ReplaceAllString(sourceText, "")
	}
	sourceText = blockCommentPattern.ReplaceAllString(sourceText, "")
	return lineCommentPattern.ReplaceAllString(sourceText, "")
}

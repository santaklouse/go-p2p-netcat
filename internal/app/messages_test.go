package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

func TestNonWebRuntimeSourcesContainNoCyrillicMessages(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test source")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	err := filepath.Walk(repositoryRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "web":
				return filepath.SkipDir
			}
			return nil
		}
		relativePath, relativeErr := filepath.Rel(repositoryRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		isGoSource := filepath.Ext(path) == ".go"
		isBrowserCoreSource := strings.HasPrefix(
			filepath.ToSlash(relativePath),
			"packages/core/src/",
		)
		if !isGoSource && !isBrowserCoreSource {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for lineNumber, line := range strings.Split(string(content), "\n") {
			if strings.IndexFunc(line, func(value rune) bool {
				return unicode.In(value, unicode.Cyrillic)
			}) >= 0 {
				t.Errorf("%s:%d contains Cyrillic text: %s", path, lineNumber+1, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindTestLocation_FindsItself(t *testing.T) {
	loc, found := findTestLocation("TestFindTestLocation_FindsItself")
	if !found {
		t.Fatal("expected to find test location, got nothing")
	}
	if !strings.HasSuffix(loc.File, "_test.go") {
		t.Errorf("expected a _test.go file, got %s", loc.File)
	}
	if loc.StartLine <= 0 {
		t.Errorf("expected positive start line, got %d", loc.StartLine)
	}
}

func TestFindTestLocation_SubtestStripped(t *testing.T) {
	// SubTest name should resolve to the parent function.
	loc, found := findTestLocation("TestFindTestLocation_FindsItself/subtest")
	if !found {
		t.Fatal("expected to find parent test location for subtest")
	}
	if loc.StartLine <= 0 {
		t.Errorf("expected positive start line, got %d", loc.StartLine)
	}
}

func TestFindTestLocation_UnknownReturnsNotFound(t *testing.T) {
	_, found := findTestLocation("TestThisDoesNotExistAtAll")
	if found {
		t.Error("expected not found for non-existent test")
	}
}

func TestOutputDirCreated(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "run_001")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sub)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

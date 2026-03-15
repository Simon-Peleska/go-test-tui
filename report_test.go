package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindTestLocation_FindsItself(t *testing.T) {
	t.Log("searching for own test function in source tree...")
	loc, found := findTestLocation("TestFindTestLocation_FindsItself")
	if !found {
		t.Fatal("expected to find test location, got nothing")
	}
	t.Logf("found at %s lines %d–%d", loc.File, loc.StartLine, loc.EndLine)
	if !strings.HasSuffix(loc.File, "_test.go") {
		t.Errorf("expected a _test.go file, got %s", loc.File)
	}
	if loc.StartLine <= 0 {
		t.Errorf("expected positive start line, got %d", loc.StartLine)
	}
}

func TestFindTestLocation_SubtestStripped(t *testing.T) {
	t.Log("subtest names should resolve to the parent function")
	loc, found := findTestLocation("TestFindTestLocation_FindsItself/subtest")
	if !found {
		t.Fatal("expected to find parent test location for subtest")
	}
	t.Logf("parent found at %s line %d", loc.File, loc.StartLine)
	if loc.StartLine <= 0 {
		t.Errorf("expected positive start line, got %d", loc.StartLine)
	}
}

func TestFindTestLocation_UnknownReturnsNotFound(t *testing.T) {
	t.Log("non-existent test should return not-found")
	_, found := findTestLocation("TestThisDoesNotExistAtAll")
	if found {
		t.Error("expected not found for non-existent test")
	}
	t.Log("correctly returned not found")
}

func TestOutputDirCreated(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "run_001")
	t.Logf("creating output dir: %s", sub)
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
	t.Logf("directory created, mode: %s", info.Mode())
}

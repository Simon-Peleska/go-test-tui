package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// testLocation holds the source position of a test function.
type testLocation struct {
	File      string
	StartLine int
	EndLine   int
}

// findTestLocation walks *_test.go files in the current directory tree and
// returns the source position of the given test function.
// For subtests (TestFoo/bar), it searches for the parent function (TestFoo).
func findTestLocation(name string) (testLocation, bool) {
	funcName := name
	if i := strings.Index(funcName, "/"); i >= 0 {
		funcName = funcName[:i]
	}

	var result testLocation
	found := false

	_ = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != funcName {
				continue
			}
			pos := fset.Position(fn.Pos())
			end := fset.Position(fn.End())
			result = testLocation{
				File:      path,
				StartLine: pos.Line,
				EndLine:   end.Line,
			}
			found = true
			return fs.SkipAll
		}
		return nil
	})

	return result, found
}

// runListReport implements the "list" subcommand.
// Usage: run_tests_tui list [-output-dir ./test_logs] [-status failed|pass|skip] [test_name]
func runListReport(args []string) {
	fset := flag.NewFlagSet("list", flag.ExitOnError)
	outputDir := fset.String("output-dir", "./test_logs", "Directory for log files")
	statusFilter := fset.String("status", "", "Filter by status: failed, pass, skip (default: all)")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: run_tests_tui list [-output-dir ./test_logs] [-status failed|pass|skip] [test_name]")
		fmt.Fprintln(os.Stderr, "\nLists tests from the last run.")
		fmt.Fprintln(os.Stderr, "If test_name is given, also prints that test's log output.")
		fset.PrintDefaults()
	}
	_ = fset.Parse(args)

	var testFilter string
	if fset.NArg() > 0 {
		testFilter = fset.Arg(0)
	}

	// Normalise status filter.
	wantAction := strings.ToLower(*statusFilter)
	if wantAction == "failed" {
		wantAction = "fail"
	} else if wantAction == "passed" {
		wantAction = "pass"
	} else if wantAction == "skipped" {
		wantAction = "skip"
	}

	runDir := filepath.Join(*outputDir, "latest")
	jsonPath := filepath.Join(runDir, "test_output.json")

	f, err := os.Open(jsonPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open %s: %v\n", jsonPath, err)
		fmt.Fprintln(os.Stderr, "Run tests first with: run_tests_tui")
		os.Exit(1)
	}
	defer f.Close()

	// Collect test final statuses in arrival order.
	type testResult struct {
		name   string
		action string // "pass", "fail", "skip"
	}
	seen := make(map[string]string) // name → action
	var order []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt goTestEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		if evt.Test == "" {
			continue
		}
		switch evt.Action {
		case "pass", "fail", "skip":
			if _, exists := seen[evt.Test]; !exists {
				order = append(order, evt.Test)
			}
			seen[evt.Test] = evt.Action
		}
	}

	// Apply status filter.
	var results []testResult
	for _, name := range order {
		action := seen[name]
		if wantAction != "" && action != wantAction {
			continue
		}
		results = append(results, testResult{name: name, action: action})
	}

	if len(results) == 0 {
		if wantAction != "" {
			fmt.Printf("No %s tests in last run.\n", *statusFilter)
		} else {
			fmt.Println("No tests in last run.")
		}
		return
	}

	// Resolve runDir to its real path (latest is a symlink).
	realRunDir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		realRunDir = runDir
	}

	label := "Tests"
	if wantAction != "" {
		label = strings.Title(*statusFilter) + " tests"
	}
	fmt.Printf("%s — run: %s\n\n", label, realRunDir)

	statusLabel := map[string]string{
		"pass": "PASS",
		"fail": "FAIL",
		"skip": "SKIP",
	}

	for _, r := range results {
		if testFilter != "" && r.name != testFilter {
			continue
		}

		safe := strings.ReplaceAll(r.name, "/", "_")
		logPath := filepath.Join(realRunDir, safe, "test_output.log")

		loc, hasLoc := findTestLocation(r.name)

		lbl := statusLabel[r.action]
		if lbl == "" {
			lbl = strings.ToUpper(r.action)
		}
		fmt.Printf("%s  %s\n", lbl, r.name)
		if hasLoc {
			fmt.Printf("      file  %s  lines %d–%d\n", loc.File, loc.StartLine, loc.EndLine)
		}
		if _, statErr := os.Stat(logPath); statErr == nil {
			fmt.Printf("      logs  %s\n", logPath)
		}

		if testFilter != "" {
			content, err := os.ReadFile(logPath)
			if err != nil {
				fmt.Printf("\n(could not read log: %v)\n", err)
			} else {
				fmt.Printf("\n%s\n", content)
			}
		}
		fmt.Println()
	}
}

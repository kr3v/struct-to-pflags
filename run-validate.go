package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

func validate() {
	cfg := parseFlags()
	if err := validateGen(cfg); err != nil {
		log.Fatalf("validation failed: %v", err)
	}
}

func validateGen(cfg *generatorConfig) error {
	// Generate expected code
	expectedCode, err := generateCode(cfg)
	if err != nil {
		log.Fatalf("failed to generate expected code: %v", err)
	}

	// Read existing file
	existingBytes, err := os.ReadFile(cfg.outputFile)
	if err != nil {
		log.Fatalf("failed to read existing file %s: %v", cfg.outputFile, err)
	}
	existingCode := string(existingBytes)

	// Normalize both strings for comparison (handle different line endings, trailing whitespace)
	expectedNormalized := normalizeCode(expectedCode)
	existingNormalized := normalizeCode(existingCode)

	// Compare
	if expectedNormalized == existingNormalized {
		fmt.Printf("✓ %s is up to date\n", cfg.outputFile)
		return nil
	}

	// Generate diff
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(existingNormalized, expectedNormalized, false)
	diffText := dmp.DiffPrettyText(diffs)

	// Print error message
	fmt.Fprintf(os.Stderr, "✗ %s is out of date\n\n", cfg.outputFile)
	fmt.Fprintf(os.Stderr, "The generated code does not match the current struct definition.\n")
	fmt.Fprintf(os.Stderr, "This usually happens when:\n")
	fmt.Fprintf(os.Stderr, "  - Struct fields were added, removed, or renamed\n")
	fmt.Fprintf(os.Stderr, "  - Field types were changed\n")
	fmt.Fprintf(os.Stderr, "  - Field comments were modified\n")
	fmt.Fprintf(os.Stderr, "  - Default values in default%s were changed\n", strings.Title(cfg.structName))
	fmt.Fprintf(os.Stderr, "\nTo fix this, run:\n")
	fmt.Fprintf(os.Stderr, "  struct-to-pflags -file %s -struct %s -output %s\n\n", cfg.filePath, cfg.structName, cfg.outputFile)
	fmt.Fprintf(os.Stderr, "Diff:\n%s\n", diffText)

	return fmt.Errorf("%s is out of date", cfg.outputFile)
}

func normalizeCode(code string) string {
	// Normalize line endings
	code = strings.ReplaceAll(code, "\r\n", "\n")
	code = strings.ReplaceAll(code, "\r", "\n")

	// Split into lines and trim trailing whitespace from each line
	lines := strings.Split(code, "\n")
	var normalized []string
	for _, line := range lines {
		normalized = append(normalized, strings.TrimRight(line, " \t"))
	}

	// Join back and trim trailing newlines
	result := strings.Join(normalized, "\n")
	result = strings.TrimRight(result, "\n")

	return result
}

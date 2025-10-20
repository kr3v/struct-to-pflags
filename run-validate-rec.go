package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// generateDirective represents a parsed go:generate directive
type generateDirective struct {
	sourceFile string
	filePath   string
	structName string
	outputFile string
	pkgName    string
	lineNumber int
}

func validateRecursive() {
	var rootDir = flag.String("dir", ".", "root directory to search for go:generate directives")
	flag.Parse()

	directives, err := findGenerateDirectives(*rootDir)
	if err != nil {
		log.Fatalf("failed to find generate directives: %v", err)
	}

	if len(directives) == 0 {
		fmt.Printf("No go:generate struct-to-pflags directives found in %s\n", *rootDir)
		return
	}

	fmt.Printf("Found %d go:generate struct-to-pflags directive(s)\n\n", len(directives))

	var failed []string
	for i, directive := range directives {
		fmt.Printf("[%d/%d] Validating %s...\n", i+1, len(directives), directive.sourceFile)

		cfg := &generatorConfig{
			filePath:    directive.filePath,
			structName:  directive.structName,
			outputFile:  directive.outputFile,
			packageName: directive.pkgName,
		}

		if err := validateGen(cfg); err != nil {
			failed = append(failed, directive.sourceFile)
			fmt.Fprintf(os.Stderr, "  ✗ FAILED: %v\n\n", err)
			continue
		}

		fmt.Printf("  ✓ OK\n\n")
	}

	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d file(s) failed validation:\n", len(failed))
		for _, f := range failed {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		os.Exit(1)
	}

	fmt.Printf("All %d file(s) validated successfully!\n", len(directives))
}

// findGenerateDirectives walks the directory tree and finds all go:generate struct-to-pflags directives
func findGenerateDirectives(rootDir string) ([]generateDirective, error) {
	var directives []generateDirective

	// Regex to match go:generate struct-to-pflags directives
	// Example: //go:generate struct-to-pflags -file config.go -struct config -output config.gen.go
	generateRegex := regexp.MustCompile(`^\s*//\s*go:generate\s+struct-to-pflags\s+(.+)$`)

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-Go files
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip generated files
		if strings.HasSuffix(path, ".gen.go") || strings.HasSuffix(path, "_gen.go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", path, err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			if match := generateRegex.FindStringSubmatch(line); match != nil {
				directive, err := parseGenerateDirective(path, match[1], lineNum)
				if err != nil {
					return fmt.Errorf("failed to parse directive in %s:%d: %w", path, lineNum, err)
				}
				directives = append(directives, directive)
			}
		}

		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return directives, nil
}

// parseGenerateDirective parses the arguments from a go:generate directive
func parseGenerateDirective(sourceFile, args string, lineNum int) (generateDirective, error) {
	directive := generateDirective{
		sourceFile: sourceFile,
		lineNumber: lineNum,
	}

	// Parse flags from the directive
	// We need to handle flags like: -file config.go -struct config -output config.gen.go
	_parts := strings.Fields(args)

	var parts []string
	for i := 0; i < len(_parts); i++ {
		ks := strings.SplitN(_parts[i], "=", 2)
		if len(ks) == 2 {
			parts = append(parts, ks[0], ks[1])
		} else {
			parts = append(parts, ks[0])
		}
	}

	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "-file":
			if i+1 >= len(parts) {
				return directive, fmt.Errorf("missing value for -file flag")
			}
			i++
			// Resolve relative path from the source file's directory
			sourceDir := filepath.Dir(sourceFile)
			directive.filePath = filepath.Join(sourceDir, parts[i])

		case "-struct":
			if i+1 >= len(parts) {
				return directive, fmt.Errorf("missing value for -struct flag")
			}
			i++
			directive.structName = parts[i]

		case "-output":
			if i+1 >= len(parts) {
				return directive, fmt.Errorf("missing value for -output flag")
			}
			i++
			// Resolve relative path from the source file's directory
			sourceDir := filepath.Dir(sourceFile)
			directive.outputFile = filepath.Join(sourceDir, parts[i])

		case "-package", "-pkg":
			if i+1 >= len(parts) {
				return directive, fmt.Errorf("missing value for -package flag")
			}
			i++
			directive.pkgName = parts[i]
		}
	}

	// Validate required fields
	if directive.filePath == "" {
		return directive, fmt.Errorf("missing -file flag")
	}
	if directive.structName == "" {
		return directive, fmt.Errorf("missing -struct flag")
	}
	if directive.outputFile == "" {
		return directive, fmt.Errorf("missing -output flag")
	}

	return directive, nil
}

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

type fieldInfo struct {
	Name            string
	Type            string
	Comment         string
	Skip            bool
	DefaultValue    string
	DefaultValueRef string
	// For embedded struct fields
	IsEmbedded       bool   // true if this is an embedded struct
	EmbeddedTypeName string // the type name (e.g., "EmbeddedDefaults")
	EmbeddedPkgAlias string // the package alias (e.g., "types")
	EmbeddedPkgPath  string // the full import path (e.g., "github.com/example/pkg/types")
}

type embeddedStructInfo struct {
	TypeName  string // e.g., "EmbeddedDefaults"
	PkgAlias  string // e.g., "types"
	PkgPath   string // e.g., "github.com/example/pkg/types"
	Fields    []fieldInfo
	FilePath  string // resolved file path
}

type generatorConfig struct {
	filePath    string
	structName  string
	outputFile  string
	packageName string
}

func parseFlags() *generatorConfig {
	var (
		filePath    = flag.String("file", "", "path to Go file containing the struct")
		structName  = flag.String("struct", "", "name of the struct to convert")
		outputFile  = flag.String("output", "", "path to output file (if empty, prints to stdout)")
		packageName = flag.String("package", "", "package name for generated code (if empty, extracted from input file)")
	)
	flag.Parse()

	if *filePath == "" || *structName == "" {
		log.Fatal("both -file and -struct flags are required")
	}

	return &generatorConfig{
		filePath:    *filePath,
		structName:  *structName,
		outputFile:  *outputFile,
		packageName: *packageName,
	}
}

func generate() {
	cfg := parseFlags()
	code, err := generateCode(cfg)
	if err != nil {
		log.Fatal(err)
	}

	// Write to file or stdout
	if cfg.outputFile != "" {
		if err := os.WriteFile(cfg.outputFile, []byte(code), 0644); err != nil {
			log.Fatalf("failed to write output file: %v", err)
		}
	} else {
		fmt.Println(code)
	}
}

func generateCode(cfg *generatorConfig) (string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, cfg.filePath, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("failed to parse file: %w", err)
	}

	// Extract package name if not provided
	pkg := cfg.packageName
	if pkg == "" {
		pkg = node.Name.Name
	}

	structFields, err := extractStructFields(node, cfg.structName)
	if err != nil {
		return "", fmt.Errorf("failed to extract struct fields: %w", err)
	}

	defaults, err := extractDefaults(node, cfg.structName)
	if err != nil {
		return "", fmt.Errorf("failed to extract defaults: %w", err)
	}

	// Merge defaults with struct fields
	for i := range structFields {
		if val, ok := defaults[structFields[i].Name]; ok {
			structFields[i].DefaultValueRef = val
		}
	}

	// Extract embedded structs
	embeddedStructs, err := extractEmbeddedStructs(node, cfg.structName, cfg.filePath)
	if err != nil {
		return "", fmt.Errorf("failed to extract embedded structs: %w", err)
	}

	// Merge defaults with embedded struct fields
	defaultVarName := "default" + strings.Title(cfg.structName)
	for i := range embeddedStructs {
		for j := range embeddedStructs[i].Fields {
			// Embedded fields are accessed directly: defaultConfig.FieldName
			embeddedStructs[i].Fields[j].DefaultValueRef = defaultVarName + "." + embeddedStructs[i].Fields[j].Name
		}
	}

	// Generate code
	return generatePflagsCode(structFields, embeddedStructs, cfg.structName, pkg), nil
}

func extractStructFields(node *ast.File, structName string) ([]fieldInfo, error) {
	var fields []fieldInfo
	var found bool

	ast.Inspect(node, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != structName {
			return true
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		found = true
		for _, field := range structType.Fields.List {
			if len(field.Names) == 0 {
				continue
			}

			fieldName := field.Names[0].Name
			fieldType := getTypeString(field.Type)

			// Check for pflags:"-" tag
			skip := false
			if field.Tag != nil {
				tagValue := field.Tag.Value
				if strings.Contains(tagValue, `pflags:"-"`) {
					skip = true
				}
			}

			// Extract comment
			comment := ""
			if field.Doc != nil && len(field.Doc.List) > 0 {
				comment = strings.TrimSpace(field.Doc.List[0].Text)
				comment = strings.TrimPrefix(comment, "//")
				comment = strings.TrimSpace(comment)
				comment = strings.Trim(comment, `"`)
			} else if field.Comment != nil && len(field.Comment.List) > 0 {
				comment = strings.TrimSpace(field.Comment.List[0].Text)
				comment = strings.TrimPrefix(comment, "//")
				comment = strings.TrimSpace(comment)
				comment = strings.Trim(comment, `"`)
			}

			fields = append(fields, fieldInfo{
				Name:    fieldName,
				Type:    fieldType,
				Comment: comment,
				Skip:    skip,
			})
		}

		return false
	})

	if !found {
		return nil, fmt.Errorf("struct %s not found", structName)
	}

	return fields, nil
}

func extractDefaults(node *ast.File, structName string) (map[string]string, error) {
	defaults := make(map[string]string)
	defaultVarName := "default" + strings.Title(structName)

	ast.Inspect(node, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			return true
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range valueSpec.Names {
				if name.Name != defaultVarName {
					continue
				}
				if i >= len(valueSpec.Values) {
					continue
				}

				compositeLit, ok := valueSpec.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}

				for _, elt := range compositeLit.Elts {
					kvExpr, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}

					keyIdent, ok := kvExpr.Key.(*ast.Ident)
					if !ok {
						continue
					}

					k, v := keyIdent.Name, defaultVarName+"."+keyIdent.Name
					defaults[k] = v
				}
			}
		}

		return true
	})

	return defaults, nil
}

// extractImports extracts import paths from an AST file and returns a map of alias -> import path
func extractImports(node *ast.File) map[string]string {
	imports := make(map[string]string)
	for _, imp := range node.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		var alias string
		if imp.Name != nil {
			alias = imp.Name.Name
		} else {
			// Use the last part of the import path as alias
			parts := strings.Split(path, "/")
			alias = parts[len(parts)-1]
		}
		imports[alias] = path
	}
	return imports
}

// resolvePackagePath resolves an import path to a filesystem directory using `go list`
func resolvePackagePath(importPath string) (string, error) {
	cmd := exec.Command("go", "list", "-f", "{{.Dir}}", importPath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve package %s: %w", importPath, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// parseEmbeddedStruct parses an embedded struct from a package directory
func parseEmbeddedStruct(pkgDir, structName string) ([]fieldInfo, error) {
	fset := token.NewFileSet()

	// Parse all Go files in the package directory
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package directory %s: %w", pkgDir, err)
	}

	var fields []fieldInfo
	found := false

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				typeSpec, ok := n.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != structName {
					return true
				}

				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					return true
				}

				found = true
				for _, field := range structType.Fields.List {
					if len(field.Names) == 0 {
						// Skip embedded structs in embedded structs for now
						continue
					}

					fieldName := field.Names[0].Name
					fieldType := getTypeString(field.Type)

					// Check for pflags:"-" tag
					skip := false
					if field.Tag != nil {
						tagValue := field.Tag.Value
						if strings.Contains(tagValue, `pflags:"-"`) {
							skip = true
						}
					}

					// Extract comment
					comment := ""
					if field.Doc != nil && len(field.Doc.List) > 0 {
						comment = strings.TrimSpace(field.Doc.List[0].Text)
						comment = strings.TrimPrefix(comment, "//")
						comment = strings.TrimSpace(comment)
						comment = strings.Trim(comment, `"`)
					} else if field.Comment != nil && len(field.Comment.List) > 0 {
						comment = strings.TrimSpace(field.Comment.List[0].Text)
						comment = strings.TrimPrefix(comment, "//")
						comment = strings.TrimSpace(comment)
						comment = strings.Trim(comment, `"`)
					}

					fields = append(fields, fieldInfo{
						Name:    fieldName,
						Type:    fieldType,
						Comment: comment,
						Skip:    skip,
					})
				}

				return false
			})
			if found {
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("struct %s not found in package %s", structName, pkgDir)
	}

	return fields, nil
}

// extractEmbeddedStructs finds embedded structs in the main struct and parses their fields
func extractEmbeddedStructs(node *ast.File, structName, sourceFilePath string) ([]embeddedStructInfo, error) {
	var embeddedStructs []embeddedStructInfo
	imports := extractImports(node)

	ast.Inspect(node, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != structName {
			return true
		}

		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		for _, field := range structType.Fields.List {
			// Embedded struct has no names
			if len(field.Names) != 0 {
				continue
			}

			// Check if it's a selector expression (pkg.Type)
			selectorExpr, ok := field.Type.(*ast.SelectorExpr)
			if !ok {
				continue
			}

			pkgIdent, ok := selectorExpr.X.(*ast.Ident)
			if !ok {
				continue
			}

			pkgAlias := pkgIdent.Name
			typeName := selectorExpr.Sel.Name
			pkgPath, ok := imports[pkgAlias]
			if !ok {
				log.Printf("warning: could not find import for package alias %s", pkgAlias)
				continue
			}

			// Resolve the package path to a filesystem directory
			pkgDir, err := resolvePackagePath(pkgPath)
			if err != nil {
				log.Printf("warning: %v", err)
				continue
			}

			// Parse the embedded struct
			fields, err := parseEmbeddedStruct(pkgDir, typeName)
			if err != nil {
				log.Printf("warning: %v", err)
				continue
			}

			embeddedStructs = append(embeddedStructs, embeddedStructInfo{
				TypeName: typeName,
				PkgAlias: pkgAlias,
				PkgPath:  pkgPath,
				Fields:   fields,
				FilePath: filepath.Join(pkgDir, "*.go"),
			})
		}

		return false
	})

	return embeddedStructs, nil
}

func getTypeString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return fmt.Sprintf("%s.%s", getTypeString(t.X), t.Sel.Name)
	case *ast.ArrayType:
		return "[]" + getTypeString(t.Elt)
	case *ast.StarExpr:
		return "*" + getTypeString(t.X)
	default:
		return "unknown"
	}
}

func getValueString(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		return strings.Trim(v.Value, `"`)
	case *ast.Ident:
		if v.Name == "true" || v.Name == "false" {
			return v.Name
		}
		return v.Name
	default:
		return ""
	}
}

func camelToKebab(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			result = append(result, '-')
		}
		result = append(result, unicode.ToLower(r))
	}
	return string(result)
}

func getPflagType(goType string) string {
	switch goType {
	case "string":
		return "String"
	case "bool":
		return "Bool"
	case "int", "int32", "int64":
		return "Int"
	case "uint", "uint32", "uint64":
		return "Uint"
	case "float32", "float64":
		return "Float64"
	case "[]string":
		return "StringSlice"
	case "time.Duration":
		return "Duration"
	default:
		return "String"
	}
}

func getFlagGetterType(goType string) string {
	switch goType {
	case "string":
		return "GetString"
	case "bool":
		return "GetBool"
	case "int", "int32", "int64":
		return "GetInt"
	case "uint", "uint32", "uint64":
		return "GetUint"
	case "float32", "float64":
		return "GetFloat64"
	case "[]string":
		return "GetStringSlice"
	case "time.Duration":
		return "GetDuration"
	default:
		return "GetString"
	}
}

func formatDefaultValue(goType, value string) string {
	if value == "" {
		switch goType {
		case "string":
			return `""`
		case "bool":
			return "false"
		case "int", "int32", "int64", "uint", "uint32", "uint64":
			return "0"
		case "float32", "float64":
			return "0.0"
		case "[]string":
			return "nil"
		default:
			return `""`
		}
	}

	switch goType {
	case "string":
		return fmt.Sprintf(`"%s"`, value)
	case "bool", "int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64":
		return value
	default:
		return fmt.Sprintf(`"%s"`, value)
	}
}

// embeddedFieldFlagName generates the flag constant name for an embedded field
// Example: EnableFeature from FeatureDefaults -> flagFeatureEnableFeatureDefaultValue
func embeddedFieldFlagName(embeddedTypeName, fieldName string) string {
	// Remove common suffixes to create a prefix
	prefix := embeddedTypeName
	prefix = strings.TrimSuffix(prefix, "Defaults")
	prefix = strings.TrimSuffix(prefix, "Options")
	prefix = strings.TrimSuffix(prefix, "Config")

	return "flag" + prefix + strings.Title(fieldName) + "DefaultValue"
}

// embeddedFieldKebabName generates the kebab-case flag name for an embedded field
func embeddedFieldKebabName(embeddedTypeName, fieldName string) string {
	prefix := embeddedTypeName
	prefix = strings.TrimSuffix(prefix, "Defaults")
	prefix = strings.TrimSuffix(prefix, "Options")
	prefix = strings.TrimSuffix(prefix, "Config")

	return camelToKebab(prefix) + "-" + camelToKebab(fieldName) + "-default-value"
}

// lowerFirst converts the first character of a string to lowercase
func lowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func generatePflagsCode(fields []fieldInfo, embeddedStructs []embeddedStructInfo, structName, packageName string) string {
	structNameC := strings.Title(structName)

	var buf bytes.Buffer

	// Add "// Code generated by struct-to-pflags; DO NOT EDIT." comment
	buf.WriteString("// Code generated by struct-to-pflags; DO NOT EDIT.\n\n")

	// Add package statement
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	// Determine required imports
	needsTime := false
	for _, field := range fields {
		if field.Type == "time.Duration" {
			needsTime = true
			break
		}
	}
	if !needsTime {
		for _, embedded := range embeddedStructs {
			for _, field := range embedded.Fields {
				if field.Type == "time.Duration" {
					needsTime = true
					break
				}
			}
			if needsTime {
				break
			}
		}
	}

	// Add imports
	buf.WriteString("import (\n")
	if needsTime {
		buf.WriteString("\t\"time\"\n\n")
	}
	buf.WriteString("\t\"github.com/spf13/pflag\"\n")
	for _, embedded := range embeddedStructs {
		buf.WriteString(fmt.Sprintf("\n\t\"%s\"\n", embedded.PkgPath))
	}
	buf.WriteString(")\n\n")

	// Generate flag constant names
	buf.WriteString("const (\n")
	for _, field := range fields {
		if field.Skip {
			continue
		}
		flagName := camelToKebab(field.Name)
		constName := "flag" + strings.Title(field.Name)
		buf.WriteString(fmt.Sprintf("\t%s = \"%s\"\n", constName, flagName))
	}
	// Generate flag constants for embedded struct fields
	for _, embedded := range embeddedStructs {
		buf.WriteString(fmt.Sprintf("\n\t// %s flags\n", embedded.TypeName))
		for _, field := range embedded.Fields {
			if field.Skip {
				continue
			}
			constName := embeddedFieldFlagName(embedded.TypeName, field.Name)
			flagName := embeddedFieldKebabName(embedded.TypeName, field.Name)
			buf.WriteString(fmt.Sprintf("\t%s = \"%s\"\n", constName, flagName))
		}
	}
	buf.WriteString(")\n\n")

	// Generate withFlags function
	buf.WriteString("func with" + structNameC + "Flags(flags *pflag.FlagSet) {\n")
	for _, field := range fields {
		if field.Skip {
			continue
		}
		flagConst := "flag" + strings.Title(field.Name)
		pflagType := getPflagType(field.Type)
		comment := field.Comment

		defaultVal := formatDefaultValue(field.Type, field.DefaultValue)
		if field.DefaultValueRef != "" {
			defaultVal = field.DefaultValueRef
		}

		buf.WriteString(fmt.Sprintf("\tflags.%s(%s, %s, %q)\n",
			pflagType, flagConst, defaultVal, comment))
	}
	// Register embedded struct flags
	for _, embedded := range embeddedStructs {
		buf.WriteString(fmt.Sprintf("\n\t// %s flags\n", embedded.TypeName))
		for _, field := range embedded.Fields {
			if field.Skip {
				continue
			}
			flagConst := embeddedFieldFlagName(embedded.TypeName, field.Name)
			pflagType := getPflagType(field.Type)
			comment := field.Comment
			if comment == "" {
				comment = fmt.Sprintf("set %s default value", camelToKebab(field.Name))
			}

			defaultVal := field.DefaultValueRef
			if defaultVal == "" {
				defaultVal = formatDefaultValue(field.Type, field.DefaultValue)
			}

			buf.WriteString(fmt.Sprintf("\tflags.%s(%s, %s, %q)\n",
				pflagType, flagConst, defaultVal, comment))
		}
	}
	buf.WriteString("}\n\n")

	// Collect skipped fields for loadConfig parameters
	var skippedFields []fieldInfo
	for _, field := range fields {
		if field.Skip {
			skippedFields = append(skippedFields, field)
		}
	}

	// Generate loadConfig function signature
	buf.WriteString("func load" + structNameC + "(flags *pflag.FlagSet")
	for _, field := range skippedFields {
		buf.WriteString(fmt.Sprintf(", %s %s", field.Name, field.Type))
	}
	buf.WriteString(fmt.Sprintf(") (*%s, error) {\n", structName))

	// Generate flag getters for regular fields
	for _, field := range fields {
		if field.Skip {
			continue
		}
		flagConst := "flag" + strings.Title(field.Name)
		getterType := getFlagGetterType(field.Type)

		buf.WriteString(fmt.Sprintf("\t%s, err := flags.%s(%s)\n", field.Name, getterType, flagConst))
		buf.WriteString("\tif err != nil {\n")
		buf.WriteString("\t\treturn nil, err\n")
		buf.WriteString("\t}\n\n")
	}

	// Generate flag getters for embedded struct fields
	for _, embedded := range embeddedStructs {
		buf.WriteString(fmt.Sprintf("\t// %s\n", embedded.TypeName))
		for _, field := range embedded.Fields {
			if field.Skip {
				continue
			}
			flagConst := embeddedFieldFlagName(embedded.TypeName, field.Name)
			getterType := getFlagGetterType(field.Type)
			// Use lowercase first char for local variable
			localVarName := lowerFirst(field.Name)

			buf.WriteString(fmt.Sprintf("\t%s, err := flags.%s(%s)\n", localVarName, getterType, flagConst))
			buf.WriteString("\tif err != nil {\n")
			buf.WriteString("\t\treturn nil, err\n")
			buf.WriteString("\t}\n\n")
		}
	}

	// Generate return statement
	buf.WriteString(fmt.Sprintf("\treturn &%s{\n", structName))
	for _, field := range fields {
		buf.WriteString(fmt.Sprintf("\t\t%s: %s,\n", field.Name, field.Name))
	}
	// Add embedded struct initialization
	for _, embedded := range embeddedStructs {
		buf.WriteString(fmt.Sprintf("\t\t%s: %s.%s{\n", embedded.TypeName, embedded.PkgAlias, embedded.TypeName))
		for _, field := range embedded.Fields {
			localVarName := lowerFirst(field.Name)
			buf.WriteString(fmt.Sprintf("\t\t\t%s: %s,\n", field.Name, localVarName))
		}
		buf.WriteString("\t\t},\n")
	}
	buf.WriteString("\t}, nil\n")
	buf.WriteString("}\n")

	// Add helper to ensure time import is used if needed
	if needsTime {
		buf.WriteString("\n// Ensure unused import is used\n")
		buf.WriteString("var _ = time.Second\n")
	}

	// Format the generated code
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("warning: failed to format code: %v", err)
		return buf.String()
	}

	return string(formatted)
}

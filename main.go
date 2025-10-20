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
}

func main() {
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

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, *filePath, nil, parser.ParseComments)
	if err != nil {
		log.Fatalf("failed to parse file: %v", err)
	}

	// Extract package name if not provided
	pkg := *packageName
	if pkg == "" {
		pkg = node.Name.Name
	}

	structFields, err := extractStructFields(node, *structName)
	if err != nil {
		log.Fatalf("failed to extract struct fields: %v", err)
	}

	defaults, err := extractDefaults(node, *structName)
	if err != nil {
		log.Fatalf("failed to extract defaults: %v", err)
	}

	// Merge defaults with struct fields
	for i := range structFields {
		if val, ok := defaults[structFields[i].Name]; ok {
			structFields[i].DefaultValueRef = val
		}
	}

	// Generate code
	code := generatePflagsCode(structFields, *structName, pkg)

	// Write to file or stdout
	if *outputFile != "" {
		if err := os.WriteFile(*outputFile, []byte(code), 0644); err != nil {
			log.Fatalf("failed to write output file: %v", err)
		}
	} else {
		fmt.Println(code)
	}
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

func generatePflagsCode(fields []fieldInfo, structName, packageName string) string {
	var buf bytes.Buffer

	// Add "// Code generated by struct-to-pflags; DO NOT EDIT." comment
	buf.WriteString("// Code generated by struct-to-pflags; DO NOT EDIT.\n\n")

	// Add package statement
	buf.WriteString(fmt.Sprintf("package %s\n\n", packageName))

	// Add imports
	buf.WriteString("import (\n")
	buf.WriteString("\t\"github.com/spf13/cobra\"\n")
	buf.WriteString("\t\"github.com/spf13/pflag\"\n")
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
	buf.WriteString(")\n\n")

	// Generate withFlags function
	buf.WriteString("func withFlags(cmd *cobra.Command) *cobra.Command {\n")
	buf.WriteString("\tpflags := cmd.PersistentFlags()\n")
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

		buf.WriteString(fmt.Sprintf("\tpflags.%s(%s, %s, %q)\n",
			pflagType, flagConst, defaultVal, comment))
	}
	buf.WriteString("\treturn cmd\n")
	buf.WriteString("}\n\n")

	// Collect skipped fields for loadConfig parameters
	var skippedFields []fieldInfo
	for _, field := range fields {
		if field.Skip {
			skippedFields = append(skippedFields, field)
		}
	}

	// Generate loadConfig function signature
	buf.WriteString("func loadConfig(flags *pflag.FlagSet")
	for _, field := range skippedFields {
		buf.WriteString(fmt.Sprintf(", %s %s", field.Name, field.Type))
	}
	buf.WriteString(fmt.Sprintf(") (*%s, error) {\n", structName))

	// Generate flag getters
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

	// Generate return statement
	buf.WriteString(fmt.Sprintf("\treturn &%s{\n", structName))
	for _, field := range fields {
		buf.WriteString(fmt.Sprintf("\t\t%s: %s,\n", field.Name, field.Name))
	}
	buf.WriteString("\t}, nil\n")
	buf.WriteString("}\n")

	// Format the generated code
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		log.Printf("warning: failed to format code: %v", err)
		return buf.String()
	}

	return string(formatted)
}

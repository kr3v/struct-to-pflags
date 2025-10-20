# Struct to Pflags Generator

A code generator tool that converts Go structs into pflags (Persistent Flags) code for command-line flag parsing.

## Features

- Automatically generates pflag definitions from Go struct fields
- Extracts default values from `default<StructName>` variables
- Respects `pflags:"-"` struct tags to skip fields
- Preserves field comments as flag descriptions
- Generates both `withFlags` and `loadConfig` functions
- Supports multiple Go types (string, bool, int, uint, float64, []string)

## Installation

```bash
go install github.com/kr3v/struct-to-pflags@latest
```

## Usage

### Command Line

```bash
struct-to-pflags -file <path-to-file.go> -struct <StructName> [-output <output-file.go>] [-package <package-name>]
```

### Using with go:generate

Add a `go:generate` directive at the top of your Go file:

```go
//go:generate go run ./main.go -file config.go -struct config -output config.gen.go

package mypackage

type config struct {
    // your struct fields...
}
```

The package name will be automatically extracted from the input file. You can override it with `-package` flag if needed.

Then run:

```bash
go generate ./...
```

This will automatically generate `config.gen.go` with the pflags code.

### Example

Check the [example directory](./example) for a complete example.

## Supported Types

- `string` → `pflags.String()` / `flags.GetString()`
- `bool` → `pflags.Bool()` / `flags.GetBool()`
- `int`, `int32`, `int64` → `pflags.Int()` / `flags.GetInt()`
- `uint`, `uint32`, `uint64` → `pflags.Uint()` / `flags.GetUint()`
- `float32`, `float64` → `pflags.Float64()` / `flags.GetFloat64()`
- `[]string` → `pflags.StringSlice()` / `flags.GetStringSlice()`

## Field Naming Convention

Field names are automatically converted from camelCase to kebab-case for flag names:
- `logFile` → `log-file`
- `enableLogs` → `enable-logs`

## Skipping Fields

Use the `pflags:"-"` struct tag to skip fields:

```go
type config struct {
    apiKey  string             // will be included
    version string `pflags:"-"` // will be skipped
}
```

Skipped fields will be added as parameters to the `loadConfig` function instead.

## Default Values

The generator looks for a variable named `default<StructName>` to extract default values:

```go
type config struct {
    timeout int
}

var defaultConfig = config{
    timeout: 30,
}
```

If no default variable is found, type-appropriate zero values are used.

## Linter

```go
struct-to-pflags validate-rec -dir <directory>
```

Looks for all the `go:generate` directives in the specified directory (recursively) and validates the generated files
against the source structs.

Check [validate-rec-output](example/validate-rec-output) for an example output.

```shell
$ struct-to-pflags validate-rec -dir=./example >./example/validate-rec-output 2>&1; echo "exit_code =" $? >> ./example/validate-rec-output
```
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
cd hack/dima-stuff/struct-to-pflags
go build -o struct-to-pflags
```

## Usage

### Command Line

```bash
./struct-to-pflags -file <path-to-file.go> -struct <StructName> [-output <output-file.go>] [-package <package-name>]
```

### Using with go:generate

Add a `go:generate` directive at the top of your Go file:

```go
//go:generate go run ../../hack/dima-stuff/struct-to-pflags/main.go -file config.go -struct config -output config_flags.go

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

This will automatically generate `config_flags.go` with the pflags code.

### Example

Given a file `example.go`:

```go
package main

type config struct {
    // "path to file where logs will be written"
    logFile         string
    // "enable debug mode"
    debug           bool
    // "port number to listen on"
    port            int
    // "internal version field"
    version         string `pflags:"-"`
}

var defaultConfig = config{
    logFile: "/var/log/app.log",
    debug:   false,
    port:    8080,
    version: "v1.0.0",
}
```

Run the generator:

```bash
./struct-to-pflags -file example.go -struct config
```

Output:

```go
package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	flagLogFile = "log-file"
	flagDebug   = "debug"
	flagPort    = "port"
)

func withFlags(cmd *cobra.Command) *cobra.Command {
	pflags := cmd.PersistentFlags()
	pflags.String(flagLogFile, "/var/log/app.log", "path to file where logs will be written")
	pflags.Bool(flagDebug, false, "enable debug mode")
	pflags.Int(flagPort, 8080, "port number to listen on")
	return cmd
}

func loadConfig(flags *pflag.FlagSet, version string) (*config, error) {
	logFile, err := flags.GetString(flagLogFile)
	if err != nil {
		return nil, err
	}

	debug, err := flags.GetBool(flagDebug)
	if err != nil {
		return nil, err
	}

	port, err := flags.GetInt(flagPort)
	if err != nil {
		return nil, err
	}

	return &config{
		logFile: logFile,
		debug:   debug,
		port:    port,
		version: version,
	}, nil
}
```

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
- `enableCriuLogs` → `enable-criu-logs`
- `criProxyRunDir` → `cri-proxy-run-dir`

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

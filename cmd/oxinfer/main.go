package main

import (
    "encoding/json"
    "fmt"
    "os"
    "crypto/sha256"
    "path/filepath"
    "time"

    "github.com/garaekz/oxinfer/internal/cli"
    "github.com/garaekz/oxinfer/internal/emitter"
    "github.com/garaekz/oxinfer/internal/manifest"
)

const version = "0.1.0"

// Type aliases for imported implementations
type Manifest = manifest.Manifest
type Delta = emitter.Delta

// Interface aliases for dependency injection
type ManifestLoader = manifest.ManifestLoader
type DeltaEmitter = emitter.DeltaEmitter

func main() {
	exitCode := run(os.Args[1:])
	os.Exit(int(exitCode))
}

// run executes the main CLI logic and returns the appropriate exit code
func run(args []string) cli.ExitCode {
    config, err := cli.ParseFlags(args)
    if err != nil {
        printError(err, false)
        if cliErr, ok := err.(*cli.CLIError); ok {
            return cli.ExitCode(cliErr.ExitCode)
        }
        return cli.ExitInternal
    }

	// Handle special flags first
	if config.ShouldShowVersion() {
		fmt.Printf("oxinfer version %s\n", version)
		return cli.ExitOK
	}

	if config.ShouldShowHelp() {
		// Help is handled by the flag package's Usage function
		return cli.ExitOK
	}

    // Warn if both stdin is piped and a manifest path is provided; flag wins per precedence
    if config.ManifestPath != "" && config.ManifestPath != "-" && cli.StdinIsPiped() {
        if config.ShouldLogWarn() {
            fmt.Fprintln(os.Stderr, "warning: stdin input detected but --manifest is set; ignoring stdin")
        }
    }

    // Execute the main analysis workflow
    if err := execute(config); err != nil {
        printError(err, config.NoColor)
        if cliErr, ok := err.(*cli.CLIError); ok {
            return cli.ExitCode(cliErr.ExitCode)
        }
        return cli.ExitInternal
    }

	return cli.ExitOK
}

// execute runs the main analysis workflow
// Initial implementation that demonstrates CLI orchestration
func execute(config *cli.CLIConfig) error {
    // Get manifest reader
    manifestReader, err := config.GetManifestReader()
    if err != nil {
        return err
    }
	defer func() {
		if manifestReader != os.Stdin {
			manifestReader.Close()
		}
	}()

	// Use real implementations from other workers
	validator := manifest.NewValidator()
	loader := manifest.NewLoader(validator)
	emitter := emitter.NewJSONEmitter()

	// Load and validate manifest using the real manifest loader
	// This ensures schema validation and path validation occur
    var manifest *Manifest
    // Input precedence:
    // - If --manifest -: read from stdin
    // - Else if --manifest <path>: read from file
    // - Else if stdin is piped: read from stdin
    // - Else: error (no input provided)
    switch {
    case config.ManifestPath == "-":
        manifest, err = loader.LoadFromReader(manifestReader)
    case config.ManifestPath != "":
        manifest, err = loader.LoadFromFile(config.ManifestPath)
    case cli.StdinIsPiped():
        manifest, err = loader.LoadFromReader(manifestReader)
    default:
        return cli.NewInputError("no manifest provided: set --manifest <path> or pipe JSON to stdin")
    }
    if err != nil {
        return err
    }

	// In this version, we validate the manifest but only emit a stub delta
	// Future versions will use the manifest data for actual analysis
	_ = manifest // Suppress unused variable warning - manifest is validated but not yet used

    // Generate schema-compliant delta output
    delta, err := emitter.EmitStub()
    if err != nil {
        return err
    }
    // Apply stamp/version
    if config.Stamp {
        ts := time.Now().UTC().Format(time.RFC3339)
        delta.Meta.GeneratedAt = &ts
    }
    ver := version
    delta.Meta.Version = &ver

    // Marshal deterministic JSON
    data, err := emitter.MarshalDeterministic(delta)
    if err != nil {
        return cli.WrapInternalError("failed to marshal delta", err)
    }

    // Print canonical hash if requested
    if config.PrintHash {
        canon, err := emitter.CanonicalBytes(delta)
        if err != nil {
            return cli.WrapInternalError("failed to produce canonical bytes", err)
        }
        sum := sha256.Sum256(canon)
        fmt.Fprintf(os.Stderr, "canonical_sha256=%x\n", sum)
    }

    // Write output: stdout or file; if file, write atomically
    if config.IsStdoutOutput() || config.OutputPath == "-" {
        if _, err := os.Stdout.Write(data); err != nil {
            return cli.WrapInternalError("failed to write JSON to stdout", err)
        }
    } else {
        dir := filepath.Dir(config.OutputPath)
        base := filepath.Base(config.OutputPath)
        tmp := filepath.Join(dir, "."+base+".tmp")
        if err := os.WriteFile(tmp, data, 0644); err != nil {
            return cli.WrapInternalError("failed to write temp output file", err)
        }
        if err := os.Rename(tmp, config.OutputPath); err != nil {
            return cli.WrapInternalError("failed to atomically rename output file", err)
        }
    }

	return nil
}

// printError prints an error message to stderr with optional color formatting
func printError(err error, noColor bool) {
	if cliErr, ok := err.(*cli.CLIError); ok {
		// Print structured CLI errors as JSON to stderr
		jsonBytes, jsonErr := json.Marshal(cliErr)
		if jsonErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
			return
		}
		fmt.Fprintf(os.Stderr, "%s\n", string(jsonBytes))
		return
	}

	// Print generic errors
	fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
}

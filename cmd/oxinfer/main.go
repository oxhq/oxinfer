package main

import (
	"encoding/json"
	"fmt"
	"os"

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
// Sprint 1: Stub implementation that demonstrates CLI orchestration
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
	if config.IsStdinInput() {
		manifest, err = loader.LoadFromReader(manifestReader)
	} else {
		manifest, err = loader.LoadFromFile(config.ManifestPath)
	}
	if err != nil {
		return err
	}

	// For Sprint 1, we validate the manifest but only emit a stub delta
	// Future sprints will use the manifest data for actual analysis
	_ = manifest // Suppress unused variable warning - manifest is validated but not yet used

	// Generate schema-compliant delta output
	delta, err := emitter.EmitStub()
	if err != nil {
		return err
	}

	// Get output writer
	outputWriter, err := config.GetOutputWriter()
	if err != nil {
		return err
	}
	defer func() {
		if outputWriter != os.Stdout {
			outputWriter.Close()
		}
	}()

	// Write JSON output
	if err := emitter.WriteJSON(outputWriter, delta); err != nil {
		return cli.WrapInternalError("failed to write delta output", err)
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
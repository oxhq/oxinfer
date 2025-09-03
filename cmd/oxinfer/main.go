package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/garaekz/oxinfer/internal/cli"
	"github.com/garaekz/oxinfer/internal/emitter"
	"github.com/garaekz/oxinfer/internal/manifest"
	"github.com/garaekz/oxinfer/internal/pipeline"
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

// execute runs the main analysis workflow using the complete pipeline orchestrator
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
	jsonEmitter := emitter.NewJSONEmitter()

	// Load and validate manifest using the real manifest loader
	// This ensures schema validation and path validation occur
	var manifestData *Manifest
	// Input precedence:
	// - If --manifest -: read from stdin
	// - Else if --manifest <path>: read from file
	// - Else if stdin is piped: read from stdin
	// - Else: error (no input provided)
	switch {
	case config.ManifestPath == "-":
		manifestData, err = loader.LoadFromReader(manifestReader)
	case config.ManifestPath != "":
		manifestData, err = loader.LoadFromFile(config.ManifestPath)
	case cli.StdinIsPiped():
		manifestData, err = loader.LoadFromReader(manifestReader)
	default:
		return cli.NewInputError("no manifest provided: set --manifest <path> or pipe JSON to stdin")
	}
	if err != nil {
		return err
	}

    // Create pipeline configuration from defaults and CLI config
    pipelineConfig := pipeline.DefaultPipelineConfig()
    pipelineConfig.EnableStamp = config.Stamp

	// Configure pipeline from manifest
	if err := pipelineConfig.ConfigureFromManifest(manifestData); err != nil {
		return cli.WrapInternalError("failed to configure pipeline from manifest", err)
	}

    // Create and configure the pipeline orchestrator
    orchestrator, err := pipeline.NewOrchestrator(pipelineConfig)
    if err != nil {
        return cli.WrapInternalError("failed to create pipeline orchestrator", err)
    }
    defer orchestrator.Close()

    // Honor cache directory precedence: if --cache-dir provided, export to env for downstream cache initialization
    if config.CacheDir != "" {
        // Resolve final cache directory using CLI precedence rules
        cacheDir := config.GetCacheDir(manifestData.Project.Root)
        _ = os.Setenv("OXINFER_CACHE_DIR", cacheDir)
    }

	// Set up progress callback if verbose mode is enabled
	if config.ShouldLogInfo() {
		orchestrator.SetProgressCallback(func(progress *pipeline.PipelineProgress) {
			if progress.PhaseStatus != "" {
				fmt.Fprintf(os.Stderr, "[%s] %s (%.1f%%)\n",
					progress.Phase.String(), progress.PhaseStatus, progress.Progress*100)
			}
		})
	}

	// Execute the complete pipeline
	ctx := context.Background()
	delta, err := orchestrator.ProcessProject(ctx, manifestData)
	if err != nil {
		return cli.WrapInternalError("pipeline execution failed", err)
	}

	// Apply stamp/version metadata
	if config.Stamp {
		ts := time.Now().UTC().Format(time.RFC3339)
		delta.Meta.GeneratedAt = &ts
	}
	ver := version
	delta.Meta.Version = &ver

	// Marshal deterministic JSON
	data, err := jsonEmitter.MarshalDeterministic(delta)
	if err != nil {
		return cli.WrapInternalError("failed to marshal delta", err)
	}

	// Print canonical hash if requested
	if config.PrintHash {
		canon, err := jsonEmitter.CanonicalBytes(delta)
		if err != nil {
			return cli.WrapInternalError("failed to produce canonical bytes", err)
		}
		sum := sha256.Sum256(canon)
		fmt.Fprintf(os.Stderr, "canonical_sha256=%x\n", sum)
	}

	// Log pipeline statistics if verbose mode is enabled
	if config.ShouldLogInfo() {
		stats := orchestrator.GetStats()
		fmt.Fprintf(os.Stderr, "Pipeline completed: %d files discovered, %d files processed, %d patterns detected, %d shapes inferred (duration: %v)\n",
			stats.FilesDiscovered, stats.FilesProcessed, stats.PatternsDetected, stats.ShapesInferred, stats.TotalDuration)
	}

    // Write output: stdout or file; if file, write atomically.
    // When a relative file path is provided, write under <projectRoot>/.oxinfer/outputs/<basename>.
    if config.IsStdoutOutput() || config.OutputPath == "-" {
        if _, err := os.Stdout.Write(data); err != nil {
            return cli.WrapInternalError("failed to write JSON to stdout", err)
        }
    } else {
        outPath := config.OutputPath
        if !filepath.IsAbs(outPath) {
            // Redirect relative output to project .oxinfer/outputs directory
            outputsDir := filepath.Join(manifestData.Project.Root, ".oxinfer", "outputs")
            if err := os.MkdirAll(outputsDir, 0o755); err != nil {
                return cli.WrapInternalError("failed to create outputs directory", err)
            }
            outPath = filepath.Join(outputsDir, filepath.Base(outPath))
        } else {
            // Ensure destination directory exists for absolute paths
            if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
                return cli.WrapInternalError("failed to create output directory", err)
            }
        }

        base := filepath.Base(outPath)
        tmp := filepath.Join(filepath.Dir(outPath), "."+base+".tmp")
        if err := os.WriteFile(tmp, data, 0644); err != nil {
            return cli.WrapInternalError("failed to write temp output file", err)
        }
        if err := os.Rename(tmp, outPath); err != nil {
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

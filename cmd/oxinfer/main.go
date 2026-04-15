//go:build goexperiment.jsonv2

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oxhq/oxinfer/internal/cli"
	"github.com/oxhq/oxinfer/internal/config"
	"github.com/oxhq/oxinfer/internal/contracts"
	"github.com/oxhq/oxinfer/internal/emitter"
	"github.com/oxhq/oxinfer/internal/logging"
	"github.com/oxhq/oxinfer/internal/manifest"
	oxpackages "github.com/oxhq/oxinfer/internal/packages"
	"github.com/oxhq/oxinfer/internal/pipeline"
	oxresponse "github.com/oxhq/oxinfer/internal/response"
)

const version = "0.1.1"

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

func getConfigLoader() *config.ConfigLoader {
	if pwd, err := os.Getwd(); err == nil {
		return config.NewConfigLoader(pwd)
	}
	return nil
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

// execute runs the main analysis workflow using the complete pipeline orchestrator
func execute(config *cli.CLIConfig) error {
	warnIfIgnoringStdin(config)

	ctx := buildExecutionContext(config)
	if config.HasRequestInput() {
		return executeRequestMode(ctx, config)
	}
	return executeManifestMode(ctx, config)
}

func buildExecutionContext(config *cli.CLIConfig) context.Context {
	ctx := context.Background()
	ctx = logging.WithVerbose(ctx, config.CreateVerboseConfig())
	ctx = logging.WithLogger(ctx, config.CreateLogger())
	return ctx
}

func executeManifestMode(ctx context.Context, config *cli.CLIConfig) error {
	manifestData, err := loadManifest(config)
	if err != nil {
		return err
	}

	delta, stats, err := executeAnalysis(ctx, config, manifestData, nil, nil)
	if err != nil {
		return err
	}

	applyStableDeltaMeta(delta, config.Stamp)
	jsonEmitter := emitter.NewJSONEmitter()
	data, err := jsonEmitter.MarshalDeterministic(delta)
	if err != nil {
		return cli.WrapInternalError("failed to marshal delta", err)
	}

	if config.PrintHash {
		canon, err := jsonEmitter.CanonicalBytes(delta)
		if err != nil {
			return cli.WrapInternalError("failed to produce canonical bytes", err)
		}
		printHash(canon)
	}

	logPipelineStats(config, stats)
	return writeOutput(config, manifestData.Project.Root, data)
}

func executeRequestMode(ctx context.Context, config *cli.CLIConfig) error {
	request, err := loadAnalysisRequest(config)
	if err != nil {
		return err
	}

	delta, stats, err := executeAnalysis(ctx, config, &request.Manifest, request.Runtime.Routes, request.Runtime.Packages)
	if err != nil {
		return err
	}

	applyStableDeltaMeta(delta, false)
	response := contracts.BuildAnalysisResponse(request, delta, version)
	if err := contracts.ValidateAnalysisResponse(response); err != nil {
		if cliErr, ok := err.(*cli.CLIError); ok && cliErr.ExitCode == cli.ExitSchema {
			return err
		}
		return cli.WrapInternalError("analysis response failed schema validation", err)
	}

	data, err := contracts.MarshalAnalysisResponse(response)
	if err != nil {
		return cli.WrapInternalError("failed to marshal analysis response", err)
	}

	if config.PrintHash {
		printHash(data)
	}

	logPipelineStats(config, stats)
	return writeOutput(config, request.Manifest.Project.Root, data)
}

func loadManifest(config *cli.CLIConfig) (*Manifest, error) {
	manifestReader, err := config.GetManifestReader()
	if err != nil {
		return nil, err
	}
	defer func() {
		if manifestReader != os.Stdin {
			_ = manifestReader.Close()
		}
	}()

	loader := manifest.NewLoader(manifest.NewValidator())
	switch {
	case config.ManifestPath == "-":
		return loader.LoadFromReader(manifestReader)
	case config.ManifestPath != "":
		return loader.LoadFromFile(config.ManifestPath)
	default:
		return nil, cli.NewInputError("no manifest provided: set --manifest <path> or --manifest - to read from stdin")
	}
}

func loadAnalysisRequest(config *cli.CLIConfig) (*contracts.AnalysisRequest, error) {
	requestReader, err := config.GetRequestReader()
	if err != nil {
		return nil, err
	}
	defer func() {
		if requestReader != os.Stdin {
			_ = requestReader.Close()
		}
	}()

	return contracts.LoadAnalysisRequestFromReader(requestReader)
}

func executeAnalysis(ctx context.Context, config *cli.CLIConfig, manifestData *Manifest, runtimeRoutes []contracts.RuntimeRoute, runtimePackages []contracts.RuntimePackage) (*Delta, *pipeline.PipelineStats, error) {
	detectedPackages, err := oxpackages.DetectInstalledPackages(manifestData.Project.Root, runtimePackages)
	if err != nil {
		return nil, nil, cli.WrapInternalError("failed to detect installed packages", err)
	}
	runtimeActionPackages, err := oxpackages.DetectRuntimeActionPackages(manifestData.Project.Root, runtimeRoutes)
	if err != nil {
		return nil, nil, cli.WrapInternalError("failed to resolve runtime action packages", err)
	}
	oxpackages.MergeVendorWhitelist(manifestData, detectedPackages)
	oxpackages.MergeVendorWhitelist(manifestData, runtimeActionPackages)
	oxpackages.EnsureVendorScanTargets(manifestData)

	delta, stats, err := runPipeline(ctx, config, manifestData)
	if err != nil {
		return nil, nil, err
	}

	if err := oxpackages.EnrichDelta(ctx, manifestData, detectedPackages, delta); err != nil {
		return nil, nil, cli.WrapInternalError("failed to enrich delta with package-aware analysis", err)
	}
	if err := oxresponse.EnrichDelta(ctx, manifestData, delta); err != nil {
		return nil, nil, cli.WrapInternalError("failed to enrich delta with resource response analysis", err)
	}

	return delta, stats, nil
}

func runPipeline(ctx context.Context, config *cli.CLIConfig, manifestData *Manifest) (*Delta, *pipeline.PipelineStats, error) {
	pipelineConfig := pipeline.DefaultPipelineConfig()
	pipelineConfig.EnableStamp = config.Stamp

	if err := pipelineConfig.ConfigureFromManifest(manifestData); err != nil {
		return nil, nil, cli.WrapInternalError("failed to configure pipeline from manifest", err)
	}

	orchestrator, err := pipeline.NewOrchestrator(pipelineConfig)
	if err != nil {
		return nil, nil, cli.WrapInternalError("failed to create pipeline orchestrator", err)
	}
	defer orchestrator.Close()

	if config.CacheDir != "" {
		cacheDir := config.GetCacheDir(manifestData.Project.Root)
		_ = os.Setenv("OXINFER_CACHE_DIR", cacheDir)
	}

	if config.ShouldLogInfo() {
		orchestrator.SetProgressCallback(func(progress *pipeline.PipelineProgress) {
			if progress.PhaseStatus != "" {
				fmt.Fprintf(os.Stderr, "[%s] %s (%.1f%%)\n",
					progress.Phase.String(), progress.PhaseStatus, progress.Progress*100)
			}
		})
	}

	delta, err := orchestrator.ProcessProject(ctx, manifestData)
	if err != nil {
		return nil, nil, cli.WrapInternalError("pipeline execution failed", err)
	}

	return delta, orchestrator.GetStats(), nil
}

func applyStableDeltaMeta(delta *Delta, includeStamp bool) {
	if includeStamp {
		ts := time.Now().UTC().Format(time.RFC3339)
		delta.Meta.GeneratedAt = &ts
	} else {
		delta.Meta.GeneratedAt = nil
	}
	ver := version
	delta.Meta.Version = &ver
	delta.Meta.Stats.DurationMs = 0
}

func logPipelineStats(config *cli.CLIConfig, stats *pipeline.PipelineStats) {
	if !config.ShouldLogInfo() || stats == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "Pipeline completed: %d files discovered, %d files processed, %d patterns detected, %d shapes inferred (duration: %v)\n",
		stats.FilesDiscovered, stats.FilesProcessed, stats.PatternsDetected, stats.ShapesInferred, stats.TotalDuration)
}

func printHash(data []byte) {
	sum := sha256.Sum256(data)
	fmt.Fprintf(os.Stderr, "canonical_sha256=%x\n", sum)
}

func warnIfIgnoringStdin(config *cli.CLIConfig) {
	if !cli.StdinIsPiped() || !config.ShouldLogWarn() {
		return
	}

	switch {
	case config.HasRequestInput() && config.RequestPath != "-":
		fmt.Fprintln(os.Stderr, "warning: stdin input detected but --request is set; ignoring stdin")
	case !config.HasRequestInput() && config.ManifestPath != "" && config.ManifestPath != "-":
		fmt.Fprintln(os.Stderr, "warning: stdin input detected but --manifest is set; ignoring stdin")
	}
}

func writeOutput(config *cli.CLIConfig, projectRoot string, data []byte) error {
	if config.IsStdoutOutput() || config.OutputPath == "-" {
		if _, err := os.Stdout.Write(data); err != nil {
			return cli.WrapInternalError("failed to write JSON to stdout", err)
		}
		return nil
	}

	outPath := config.OutputPath
	var relativeToProject bool
	var defaultOutputDir string

	if cfgLoader := getConfigLoader(); cfgLoader != nil {
		if cfg, err := cfgLoader.Load(""); err == nil {
			relativeToProject = cfg.Output.RelativeToProject
			defaultOutputDir = cfg.Output.DefaultOutputDir
		}
	}

	if !filepath.IsAbs(outPath) {
		if relativeToProject {
			outputsDir := filepath.Join(projectRoot, ".oxinfer", "outputs")
			if err := os.MkdirAll(outputsDir, 0o755); err != nil {
				return cli.WrapInternalError("failed to create outputs directory", err)
			}
			outPath = filepath.Join(outputsDir, filepath.Base(outPath))
		} else {
			if defaultOutputDir == "." {
				pwd, _ := os.Getwd()
				outPath = filepath.Join(pwd, outPath)
			} else {
				outPath = filepath.Join(defaultOutputDir, outPath)
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return cli.WrapInternalError("failed to create output directory", err)
	}

	base := filepath.Base(outPath)
	tmp := filepath.Join(filepath.Dir(outPath), "."+base+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return cli.WrapInternalError("failed to write temp output file", err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		return cli.WrapInternalError("failed to atomically rename output file", err)
	}

	return nil
}

// printError prints an error message to stderr with optional color formatting
func printError(err error, noColor bool) {
	if cliErr, ok := err.(*cli.CLIError); ok {
		// Print structured CLI errors as JSON to stderr
		jsonBytes, jsonErr := json.Marshal(cliErr, json.Deterministic(true))
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

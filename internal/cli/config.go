package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oxhq/oxinfer/internal/logging"
)

// CLIConfig holds the parsed command-line configuration
type CLIConfig struct {
	// ManifestPath is the path to the manifest file
	// If empty, manifest will be read from stdin
	ManifestPath string

	// RequestPath is the path to the analysis request file.
	// If set, oxinfer runs in contract mode instead of standalone manifest mode.
	RequestPath string

	// OutputPath is the path where the delta JSON will be written
	// If empty, output will be written to stdout
	OutputPath string

	// NoColor disables colored output for error messages
	NoColor bool

	// Version indicates if version information should be displayed
	Version bool

	// Help indicates if help should be displayed
	Help bool

	// LogLevel controls verbosity: error|warn|info|debug
	LogLevel string

	// Quiet sets LogLevel=error
	Quiet bool

	// Stamp includes meta.generatedAt timestamp in output
	Stamp bool

	// PrintHash prints canonical SHA256 of the output JSON to stderr
	PrintHash bool

	// CacheDir overrides default cache directory
	CacheDir string

	// Verbose output system (user-facing progress)
	Verbose           bool     // -v, --verbose
	VerboseComponents []string // --verbose-components=indexer,broadcast

	// Structured logging system (debugging/development)
	LogComponents []string // --log-components=psr4,pipeline
	LogOutput     string   // --log-output=file|stderr|off
}

// ParseFlags parses command-line flags and returns the configuration
// It returns an error if flag parsing fails or invalid combinations are provided
func ParseFlags(args []string) (*CLIConfig, error) {
	// Create a new flag set to avoid affecting global state
	fs := flag.NewFlagSet("oxinfer", flag.ContinueOnError)
	// Set output to stdout for help/usage
	fs.SetOutput(os.Stdout)

	config := &CLIConfig{}

	// Define flags
	fs.StringVar(&config.ManifestPath, "manifest", "", "Path to manifest file or '-' to read from stdin")
	fs.StringVar(&config.RequestPath, "request", "", "Path to analysis request file or '-' to read from stdin")
	fs.StringVar(&config.OutputPath, "out", "", "Output file path (writes to stdout if not provided; use '-' for stdout)")
	fs.BoolVar(&config.NoColor, "no-color", false, "Disable colored output")
	fs.BoolVar(&config.Version, "version", false, "Show version information")
	fs.BoolVar(&config.Help, "help", false, "Show help information")
	fs.BoolVar(&config.Help, "h", false, "Show help information (short form)")
	fs.StringVar(&config.LogLevel, "log-level", "warn", "Log level: error|warn|info|debug")
	fs.BoolVar(&config.Quiet, "quiet", false, "Quiet mode (equivalent to --log-level=error)")
	fs.BoolVar(&config.Stamp, "stamp", false, "Include meta.generatedAt timestamp in output")
	fs.BoolVar(&config.PrintHash, "print-hash", false, "Print canonical SHA256 of the output to stderr")
	fs.StringVar(&config.CacheDir, "cache-dir", "", "Override cache directory (default: <project>/.oxinfer/cache/v1)")

	// Verbose output system
	fs.BoolVar(&config.Verbose, "verbose", false, "Enable verbose output to stderr")
	fs.BoolVar(&config.Verbose, "v", false, "Enable verbose output (short form)")

	// Structured logging system
	fs.StringVar(&config.LogOutput, "log-output", "off", "Logger output: off|stderr|file")

	// Parse comma-separated component lists
	var verboseComps, logComps string
	fs.StringVar(&verboseComps, "verbose-components", "", "Comma-separated list of components for verbose output (e.g., indexer,broadcast)")
	fs.StringVar(&logComps, "log-components", "", "Comma-separated list of components for structured logging (e.g., psr4,pipeline)")

	// Set custom usage function
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Oxinfer - Laravel/PHP static analysis tool\n\n")
		_, _ = fmt.Fprintf(fs.Output(), "Usage:\n")
		_, _ = fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --manifest -                 # read manifest from stdin\n")
		fmt.Fprintf(fs.Output(), "  cat manifest.json | oxinfer --manifest -\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --request -                  # read AnalysisRequest JSON from stdin\n")
		fmt.Fprintf(fs.Output(), "  cat analysis-request.json | oxinfer --request -\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json --out delta.json\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --manifest - --out -         # stdin to stdout\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nExit Codes:\n")
		fmt.Fprintf(fs.Output(), "  0  Success\n")
		fmt.Fprintf(fs.Output(), "  1  Input validation error\n")
		fmt.Fprintf(fs.Output(), "  2  Internal processing error\n")
		fmt.Fprintf(fs.Output(), "  3  Schema load/validation failure\n")
		fmt.Fprintf(fs.Output(), "  4  Hard limit exceeded\n")
		fmt.Fprintf(fs.Output(), "  5  Ownership violation (reserved)\n")
	}

	// Parse the arguments
	err := fs.Parse(args)
	if err != nil {
		// Handle the special case of help flag
		if err == flag.ErrHelp {
			config.Help = true
			// Don't return an error for help - this is expected behavior
			return config, nil
		}
		return nil, NewInputError(fmt.Sprintf("flag parsing failed: %v", err))
	}

	// Parse comma-separated component lists
	if verboseComps != "" {
		config.VerboseComponents = parseComponents(verboseComps)
	}
	if logComps != "" {
		config.LogComponents = parseComponents(logComps)
	}

	// Handle help flag after parsing
	if config.Help {
		fs.Usage()
	}

	// Normalize log level
	switch config.LogLevel {
	case "error", "warn", "info", "debug":
	default:
		config.LogLevel = "warn"
	}
	if config.Quiet {
		config.LogLevel = "error"
	}

	// If no explicit input mode was provided, treat stdin as manifest input by
	// default so piping `manifest.json | oxinfer` continues to work.
	if config.ManifestPath == "" && config.RequestPath == "" {
		config.ManifestPath = "-"
	}

	// Validate configuration (skip validation for help/version)
	if !config.Help && !config.Version {
		if err := config.Validate(); err != nil {
			return nil, err
		}
	}

	return config, nil
}

// Validate checks if the configuration is valid
func (c *CLIConfig) Validate() error {
	if c.ManifestPath != "" && c.RequestPath != "" {
		return NewInputError("--manifest and --request are mutually exclusive")
	}
	return nil
}

// IsStdinInput returns true if manifest should be read from stdin
func (c *CLIConfig) IsStdinInput() bool {
	if c.HasRequestInput() {
		return c.RequestPath == "-"
	}
	return c.ManifestPath == "" || c.ManifestPath == "-"
}

// HasRequestInput returns true when the CLI is running in analysis request mode.
func (c *CLIConfig) HasRequestInput() bool {
	return c.RequestPath != ""
}

// IsStdoutOutput returns true if output should go to stdout
func (c *CLIConfig) IsStdoutOutput() bool {
	return c.OutputPath == ""
}

// ShouldShowHelp returns true if help should be displayed
func (c *CLIConfig) ShouldShowHelp() bool {
	return c.Help
}

// ShouldShowVersion returns true if version should be displayed
func (c *CLIConfig) ShouldShowVersion() bool {
	return c.Version
}

// GetManifestReader returns a reader for the manifest input
func (c *CLIConfig) GetManifestReader() (*os.File, error) {
	if c.ManifestPath == "-" || (c.ManifestPath == "") {
		return os.Stdin, nil
	}

	file, err := os.Open(c.ManifestPath)
	if err != nil {
		return nil, WrapInputError(fmt.Sprintf("failed to open manifest file %q", c.ManifestPath), err)
	}

	return file, nil
}

// GetRequestReader returns a reader for the analysis request input.
func (c *CLIConfig) GetRequestReader() (*os.File, error) {
	if c.RequestPath == "-" {
		return os.Stdin, nil
	}

	file, err := os.Open(c.RequestPath)
	if err != nil {
		return nil, WrapInputError(fmt.Sprintf("failed to open analysis request file %q", c.RequestPath), err)
	}

	return file, nil
}

// GetOutputWriter returns a writer for the delta output
func (c *CLIConfig) GetOutputWriter() (*os.File, error) {
	if c.IsStdoutOutput() || c.OutputPath == "-" {
		return os.Stdout, nil
	}

	file, err := os.Create(c.OutputPath)
	if err != nil {
		return nil, WrapInternalError(fmt.Sprintf("failed to create output file %q", c.OutputPath), err)
	}

	return file, nil
}

// Logging helpers
func (c *CLIConfig) ShouldLogWarn() bool {
	return c.LogLevel == "warn" || c.LogLevel == "info" || c.LogLevel == "debug"
}

func (c *CLIConfig) ShouldLogInfo() bool {
	return c.LogLevel == "info" || c.LogLevel == "debug"
}

func (c *CLIConfig) ShouldLogDebug() bool {
	return c.LogLevel == "debug"
}

// StdinIsPiped reports whether stdin is coming from a pipe or file (not a terminal)
func StdinIsPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}

// GetCacheDir resolves the cache directory path with proper precedence:
// 1. --cache-dir flag, 2. OXINFER_CACHE_DIR env, 3. default <projectRoot>/.oxinfer/cache/v1/
func (c *CLIConfig) GetCacheDir(projectRoot string) string {
	// Priority 1: --cache-dir flag (highest priority)
	if c.CacheDir != "" {
		if filepath.IsAbs(c.CacheDir) {
			return c.CacheDir
		}
		// Convert relative flag path to absolute
		if absPath, err := filepath.Abs(c.CacheDir); err == nil {
			return absPath
		}
		return c.CacheDir // fallback to original if Abs fails
	}

	// Priority 2: OXINFER_CACHE_DIR environment variable
	if envCacheDir := os.Getenv("OXINFER_CACHE_DIR"); envCacheDir != "" {
		if filepath.IsAbs(envCacheDir) {
			return envCacheDir
		}
		// Convert relative env path to absolute
		if absPath, err := filepath.Abs(envCacheDir); err == nil {
			return absPath
		}
		return envCacheDir // fallback to original if Abs fails
	}

	// Priority 3: Default path <projectRoot>/.oxinfer/cache/v1/ (lowest priority)
	defaultPath := filepath.Join(projectRoot, ".oxinfer", "cache", "v1")
	if filepath.IsAbs(defaultPath) {
		return defaultPath
	}
	// Convert relative default path to absolute
	if absPath, err := filepath.Abs(defaultPath); err == nil {
		return absPath
	}
	return defaultPath // fallback to original if Abs fails
}

// parseComponents splits a comma-separated string into component names
// and trims whitespace from each component
func parseComponents(input string) []string {
	if input == "" {
		return nil
	}

	var components []string
	for _, comp := range strings.Split(input, ",") {
		comp = strings.TrimSpace(comp)
		if comp != "" {
			components = append(components, comp)
		}
	}
	return components
}

// CreateVerboseConfig creates a VerboseConfig from CLI flags
func (c *CLIConfig) CreateVerboseConfig() *logging.VerboseConfig {
	if !c.Verbose {
		return &logging.VerboseConfig{Enabled: false}
	}

	var components map[string]bool
	if len(c.VerboseComponents) > 0 {
		components = make(map[string]bool)
		for _, comp := range c.VerboseComponents {
			components[comp] = true
		}
	}

	return &logging.VerboseConfig{
		Enabled:    true,
		Components: components,
	}
}

// CreateLogger creates a structured logger from CLI flags
func (c *CLIConfig) CreateLogger() logging.Logger {
	if c.LogOutput == "off" {
		return logging.NewNoOpLogger()
	}

	var output io.Writer
	switch c.LogOutput {
	case "stderr":
		output = os.Stderr
	case "file":
		// For now, default to stderr. In future could make configurable
		output = os.Stderr
	default:
		return logging.NewNoOpLogger()
	}

	// Map CLI log level to logging.LogLevel
	var level logging.LogLevel
	switch c.LogLevel {
	case "error":
		level = logging.LogLevelError
	case "warn":
		level = logging.LogLevelWarn
	case "info":
		level = logging.LogLevelInfo
	case "debug":
		level = logging.LogLevelDebug
	default:
		level = logging.LogLevelWarn
	}

	baseLogger := logging.NewStructuredLogger(output, level)

	// If specific components configured, wrap with component filter
	if len(c.LogComponents) > 0 {
		enabledComps := make(map[string]bool)
		for _, comp := range c.LogComponents {
			enabledComps[comp] = true
		}
		return &componentFilterLogger{
			base:    baseLogger,
			enabled: enabledComps,
		}
	}

	return baseLogger
}

// componentFilterLogger wraps a logger and filters by component
type componentFilterLogger struct {
	base             logging.Logger
	enabled          map[string]bool
	currentComponent string
}

func (c *componentFilterLogger) shouldLog() bool {
	if c.enabled == nil {
		return true // No filter, log everything
	}
	return c.enabled[c.currentComponent]
}

func (c *componentFilterLogger) Error(message string, data map[string]interface{}) {
	if c.shouldLog() {
		c.base.Error(message, data)
	}
}

func (c *componentFilterLogger) Warn(message string, data map[string]interface{}) {
	if c.shouldLog() {
		c.base.Warn(message, data)
	}
}

func (c *componentFilterLogger) Info(message string, data map[string]interface{}) {
	if c.shouldLog() {
		c.base.Info(message, data)
	}
}

func (c *componentFilterLogger) Debug(message string, data map[string]interface{}) {
	if c.shouldLog() {
		c.base.Debug(message, data)
	}
}

func (c *componentFilterLogger) Trace(message string, data map[string]interface{}) {
	if c.shouldLog() {
		c.base.Trace(message, data)
	}
}

func (c *componentFilterLogger) WithComponent(component string) logging.Logger {
	return &componentFilterLogger{
		base:             c.base.WithComponent(component),
		enabled:          c.enabled,
		currentComponent: component,
	}
}

func (c *componentFilterLogger) WithPhase(phase string) logging.Logger {
	return &componentFilterLogger{
		base:             c.base.WithPhase(phase),
		enabled:          c.enabled,
		currentComponent: c.currentComponent,
	}
}

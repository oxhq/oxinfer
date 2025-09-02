package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// CLIConfig holds the parsed command-line configuration
type CLIConfig struct {
	// ManifestPath is the path to the manifest file
	// If empty, manifest will be read from stdin
	ManifestPath string

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
    fs.StringVar(&config.ManifestPath, "manifest", "", "Path to manifest file (reads from stdin if not provided)")
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

	// Set custom usage function
    fs.Usage = func() {
        fmt.Fprintf(fs.Output(), "Oxinfer - Laravel/PHP static analysis tool\n\n")
        fmt.Fprintf(fs.Output(), "Usage:\n")
        fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json\n")
        fmt.Fprintf(fs.Output(), "  oxinfer --manifest -                # read manifest from stdin\n")
        fmt.Fprintf(fs.Output(), "  cat manifest.json | oxinfer         # stdin when piped\n")
        fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json --out delta.json\n")
        fmt.Fprintf(fs.Output(), "  oxinfer --out -                      # write delta to stdout\n\n")
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
		return nil, NewInputError(fmt.Sprintf("flag parsing failed: %v", err))
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

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate checks if the configuration is valid
func (c *CLIConfig) Validate() error {
    // No validation errors in initial version - all combinations are valid
    // Future versions may add validation rules here
    return nil
}

// IsStdinInput returns true if manifest should be read from stdin
func (c *CLIConfig) IsStdinInput() bool {
    return c.ManifestPath == "" || c.ManifestPath == "-"
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

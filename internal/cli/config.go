package cli

import (
	"flag"
	"fmt"
	"os"
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
	fs.StringVar(&config.OutputPath, "out", "", "Output file path (writes to stdout if not provided)")
	fs.BoolVar(&config.NoColor, "no-color", false, "Disable colored output")
	fs.BoolVar(&config.Version, "version", false, "Show version information")
	fs.BoolVar(&config.Help, "help", false, "Show help information")
	fs.BoolVar(&config.Help, "h", false, "Show help information (short form)")

	// Set custom usage function
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Oxinfer - Laravel/PHP static analysis tool\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json\n")
		fmt.Fprintf(fs.Output(), "  cat manifest.json | oxinfer\n")
		fmt.Fprintf(fs.Output(), "  oxinfer --manifest manifest.json --out delta.json\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nExit Codes:\n")
		fmt.Fprintf(fs.Output(), "  0  Success\n")
		fmt.Fprintf(fs.Output(), "  1  Input validation error\n")
		fmt.Fprintf(fs.Output(), "  2  Internal processing error\n")
		fmt.Fprintf(fs.Output(), "  3  Schema load/validation failure\n")
		fmt.Fprintf(fs.Output(), "  4  Hard limit exceeded\n")
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

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate checks if the configuration is valid
func (c *CLIConfig) Validate() error {
	// No validation errors for Sprint 1 - all combinations are valid
	// Future sprints may add validation rules here
	return nil
}

// IsStdinInput returns true if manifest should be read from stdin
func (c *CLIConfig) IsStdinInput() bool {
	return c.ManifestPath == ""
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
	if c.IsStdinInput() {
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
	if c.IsStdoutOutput() {
		return os.Stdout, nil
	}

	file, err := os.Create(c.OutputPath)
	if err != nil {
		return nil, WrapInternalError(fmt.Sprintf("failed to create output file %q", c.OutputPath), err)
	}

	return file, nil
}
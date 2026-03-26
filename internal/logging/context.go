//go:build goexperiment.jsonv2

package logging

import (
	"context"
	"fmt"
	"os"
)

// Context keys for logger and verbose systems
type loggerKey struct{}
type verboseKey struct{}

// Logger context injection and extraction
func WithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

func FromContext(ctx context.Context) Logger {
	if logger, ok := ctx.Value(loggerKey{}).(Logger); ok {
		return logger
	}
	return NewNoOpLogger() // Fallback silencioso
}

// Verbose context injection and extraction
type VerboseConfig struct {
	Enabled    bool
	Components map[string]bool // nil means all components enabled
}

func WithVerbose(ctx context.Context, config *VerboseConfig) context.Context {
	return context.WithValue(ctx, verboseKey{}, config)
}

func ShouldVerbose(ctx context.Context, component string) bool {
	config, ok := ctx.Value(verboseKey{}).(*VerboseConfig)
	if !ok || !config.Enabled {
		return false
	}
	
	// If no specific components configured, enable all
	if config.Components == nil {
		return true
	}
	
	// Check if this specific component is enabled
	return config.Components[component]
}

// Helper functions for common logging patterns
func DebugFromContext(ctx context.Context, component, message string, data map[string]interface{}) {
	FromContext(ctx).WithComponent(component).Debug(message, data)
}

func InfoFromContext(ctx context.Context, component, message string, data map[string]interface{}) {
	FromContext(ctx).WithComponent(component).Info(message, data)
}

func WarnFromContext(ctx context.Context, component, message string, data map[string]interface{}) {
	FromContext(ctx).WithComponent(component).Warn(message, data)
}

func ErrorFromContext(ctx context.Context, component, message string, data map[string]interface{}) {
	FromContext(ctx).WithComponent(component).Error(message, data)
}

// Helper para verbose output que no interfiera con stdout JSON
func VerboseFromContext(ctx context.Context, component, format string, args ...interface{}) {
	if ShouldVerbose(ctx, component) {
		fmt.Fprintf(os.Stderr, "["+component+"] "+format+"\n", args...)
	}
}

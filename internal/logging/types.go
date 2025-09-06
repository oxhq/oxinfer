//go:build goexperiment.jsonv2

package logging

import (
	"encoding/json/v2"
	"io"
	"time"
)

type LogLevel int

const (
	LogLevelSilent LogLevel = iota
	LogLevelError
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

func (l LogLevel) String() string {
	switch l {
	case LogLevelSilent:
		return "silent"
	case LogLevelError:
		return "error"
	case LogLevelWarn:
		return "warn"
	case LogLevelInfo:
		return "info"
	case LogLevelDebug:
		return "debug"
	case LogLevelTrace:
		return "trace"
	default:
		return "unknown"
	}
}

type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Phase     string                 `json:"phase"`
	Component string                 `json:"component"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Duration  *time.Duration         `json:"duration,omitempty"`
	Error     *string                `json:"error,omitempty"`
}

type Logger interface {
	Error(message string, data map[string]interface{})
	Warn(message string, data map[string]interface{})
	Info(message string, data map[string]interface{})
	Debug(message string, data map[string]interface{})
	Trace(message string, data map[string]interface{})
	WithComponent(component string) Logger
	WithPhase(phase string) Logger
}

type StructuredLogger struct {
	output    io.Writer
	level     LogLevel
	component string
	phase     string
}

func NewStructuredLogger(output io.Writer, level LogLevel) *StructuredLogger {
	return &StructuredLogger{
		output: output,
		level:  level,
	}
}

func (s *StructuredLogger) log(level LogLevel, message string, data map[string]interface{}) {
	if level > s.level {
		return
	}
	
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Component: s.component,
		Phase:     s.phase,
		Message:   message,
		Data:      data,
	}
	
	jsonBytes, err := json.Marshal(entry, json.Deterministic(true))
	if err != nil {
		return
	}
	
	s.output.Write(jsonBytes)
	s.output.Write([]byte("\n"))
}

func (s *StructuredLogger) Error(message string, data map[string]interface{}) {
	s.log(LogLevelError, message, data)
}

func (s *StructuredLogger) Warn(message string, data map[string]interface{}) {
	s.log(LogLevelWarn, message, data)
}

func (s *StructuredLogger) Info(message string, data map[string]interface{}) {
	s.log(LogLevelInfo, message, data)
}

func (s *StructuredLogger) Debug(message string, data map[string]interface{}) {
	s.log(LogLevelDebug, message, data)
}

func (s *StructuredLogger) Trace(message string, data map[string]interface{}) {
	s.log(LogLevelTrace, message, data)
}

func (s *StructuredLogger) WithComponent(component string) Logger {
	return &StructuredLogger{
		output:    s.output,
		level:     s.level,
		component: component,
		phase:     s.phase,
	}
}

func (s *StructuredLogger) WithPhase(phase string) Logger {
	return &StructuredLogger{
		output:    s.output,
		level:     s.level,
		component: s.component,
		phase:     phase,
	}
}

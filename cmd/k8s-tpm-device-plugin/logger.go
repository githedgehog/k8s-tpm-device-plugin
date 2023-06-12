package main

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewLogger(level zapcore.Level, format string, development bool) (*zap.Logger, error) {
	// we enable callers, stacktraces and functions in development mode only
	disableCaller := true
	disableStacktrace := true
	functionKey := zapcore.OmitKey
	if development {
		disableCaller = false
		disableStacktrace = false
		functionKey = "F"
	}

	// these settings will be dependent on the format
	encoding := "console"
	encodeLevel := zapcore.CapitalColorLevelEncoder
	keyConvert := func(s string) string { return s }
	if format == "json" {
		encoding = "json"
		encodeLevel = zapcore.LowercaseLevelEncoder
		keyConvert = func(s string) string { return strings.ToLower(s) }
	}

	cfg := zap.Config{
		Level:             zap.NewAtomicLevelAt(level),
		Development:       development,
		DisableCaller:     disableCaller,
		DisableStacktrace: disableStacktrace,
		Encoding:          encoding,
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        keyConvert("T"),
			LevelKey:       keyConvert("L"),
			NameKey:        keyConvert("N"),
			CallerKey:      keyConvert("C"),
			FunctionKey:    keyConvert(functionKey),
			MessageKey:     keyConvert("M"),
			StacktraceKey:  keyConvert("S"),
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    encodeLevel,
			EncodeTime:     zapcore.RFC3339TimeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
	return cfg.Build()
}

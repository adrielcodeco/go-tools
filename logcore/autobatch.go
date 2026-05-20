package logcore

import (
	autobatch "github.com/adrielcodeco/go-tools/gormautobatch"
)

// AutobatchLogger returns a function compatible with autobatch.Config.Logger
// that routes log entries to l (or the global logger when l is nil).
// The autobatch package documents its Logger args as slog/zap-sugared
// key-value pairs, which maps directly to zap's SugaredLogger.Infow etc.
//
//	db.Use(autobatch.New(autobatch.Config{
//	    Logger: logcore.AutobatchLogger(nil), // uses global logger
//	}))
func AutobatchLogger(l *Logger) func(level autobatch.LogLevel, msg string, args ...any) {
	return func(level autobatch.LogLevel, msg string, args ...any) {
		logger := l
		if logger == nil {
			logger = globalLogger()
		}
		sugar := logger.Sugar()
		switch level {
		case autobatch.LogLevelDebug:
			sugar.Debugw(msg, args...)
		case autobatch.LogLevelInfo:
			sugar.Infow(msg, args...)
		case autobatch.LogLevelWarn:
			sugar.Warnw(msg, args...)
		default:
			sugar.Errorw(msg, args...)
		}
	}
}

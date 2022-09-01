package logger

import (
	"os"

	"github.com/phuslu/log"

	"github.com/xenking/exchange-emulator/config"
)

var Log *log.Logger

func SetGlobal(logger *log.Logger) {
	Log = logger
}

func New(cfg *config.LoggerConfig) *log.Logger {
	level := log.ParseLevel(cfg.Level)
	log.DefaultLogger.SetLevel(level)

	var w log.Writer

	cw := &log.ConsoleWriter{
		ColorOutput:    true,
		QuoteString:    true,
		EndWithMessage: true,
		Writer:         os.Stdout,
	}

	fw := &log.FileWriter{
		Filename:   "out.log",
		TimeFormat: log.TimeFormatUnixMs,
	}

	if !cfg.DisableConsole {
		w = &log.MultiEntryWriter{cw, fw}
	} else {
		w = fw
	}

	return &log.Logger{
		Level:  level,
		Caller: cfg.WithCaller,
		Writer: w,
	}
}

func NewModule(name string) *log.Logger {
	ctx := log.NewContext(nil).Str("module", name).Value()

	return &log.Logger{
		Level:          Log.Level,
		Caller:         Log.Caller,
		FullpathCaller: Log.FullpathCaller,
		TimeField:      Log.TimeField,
		TimeFormat:     Log.TimeFormat,
		Context:        ctx,
		Writer:         Log.Writer,
	}
}

func NewUser(name string) *log.Logger {
	ctx := log.NewContext(nil).Str("user", name).Value()

	return &log.Logger{
		Level:          Log.Level,
		Caller:         Log.Caller,
		FullpathCaller: Log.FullpathCaller,
		TimeField:      Log.TimeField,
		TimeFormat:     Log.TimeFormat,
		Context:        ctx,
		Writer:         Log.Writer,
	}
}

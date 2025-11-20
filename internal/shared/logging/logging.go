package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/joho/godotenv"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	logger *slog.Logger
	once   sync.Once
)

type Options struct {
	Path       string
	Level      string
	Console    bool
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
}

func Init(opt Options) {
	once.Do(func() {
		if opt.Path == "" {
			opt.Path = "./logs/rotaria.log"
		}
		if opt.Level == "" {
			opt.Level = "info"
		}
		if opt.MaxSizeMB == 0 {
			opt.MaxSizeMB = 25
		}
		if opt.MaxBackups == 0 {
			opt.MaxBackups = 5
		}
		if opt.MaxAgeDays == 0 {
			opt.MaxAgeDays = 30
		}

		_ = os.MkdirAll(filepath.Dir(opt.Path), 0755)

		lvl := parseLevel(opt.Level)
		fileWriter := &lumberjack.Logger{
			Filename:   opt.Path,
			MaxSize:    opt.MaxSizeMB,
			MaxBackups: opt.MaxBackups,
			MaxAge:     opt.MaxAgeDays,
			Compress:   false,
		}
		fileHandler := slog.NewJSONHandler(ioWriter{fileWriter}, &slog.HandlerOptions{Level: lvl})

		var handlers []slog.Handler
		handlers = append(handlers, fileHandler)

		if opt.Console {
			consoleHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
			handlers = append(handlers, consoleHandler)
		}

		multi := multiHandler(handlers...)
		logger = slog.New(multi)
		logger.Info("logger initialized",
			"path", opt.Path,
			"level", opt.Level,
			"console", opt.Console,
		)
	})
}

func L() *slog.Logger {
	if logger == nil {
		Init(Options{Console: true})
	}
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Allow lumberjack to satisfy io.Writer for handler
type ioWriter struct{ w io.Writer }

func (w ioWriter) Write(p []byte) (int, error) { return w.w.Write(p) }

type fanout struct{ hs []slog.Handler }

func (f fanout) Enabled(ctx context.Context, lvl slog.Level) bool {
	for _, h := range f.hs {
		if h.Enabled(ctx, lvl) {
			return true
		}
	}
	return false
}
func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f.hs {
		_ = h.Handle(ctx, r)
	}
	return nil
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	var nh []slog.Handler
	for _, h := range f.hs {
		nh = append(nh, h.WithAttrs(attrs))
	}
	return fanout{hs: nh}
}

func (f fanout) WithGroup(name string) slog.Handler {
	var nh []slog.Handler
	for _, h := range f.hs {
		nh = append(nh, h.WithGroup(name))
	}
	return fanout{hs: nh}
}

func BootstrapFromEnv() {
	_ = godotenv.Load()
	env := os.Getenv("ENV")
	opt := Options{
		Path:    envDefault("LOG_PATH", "./logs/rotaria.log"),
		Level:   envDefault("LOG_LEVEL", "info"),
		Console: env == "dev" || os.Getenv("LOG_CONSOLE") == "1",
	}
	Init(opt)
}

func envDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

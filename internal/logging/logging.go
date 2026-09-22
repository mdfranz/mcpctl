// Package logging provides mcpctl's opt-out, structured file logging.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	maxBytes    = 5 << 20
	backupCount = 3
)

var (
	mu      sync.Mutex
	closer  io.Closer
	logFile *os.File
)

// Init configures the process logger. Logging is disabled with
// MCPCTL_NO_LOG=1. Initialization errors are returned; callers should keep
// operating because logging is best-effort.
func Init() (io.Closer, error) {
	mu.Lock()
	defer mu.Unlock()
	if closer != nil {
		return closer, nil
	}
	if os.Getenv("MCPCTL_NO_LOG") == "1" {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return nopCloser{}, nil
	}
	dir, err := logDir()
	if err != nil {
		return nopCloser{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nopCloser{}, err
	}
	path := filepath.Join(dir, "mcpctl.log")
	if err := rotate(path); err != nil {
		return nopCloser{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nopCloser{}, err
	}
	_ = f.Chmod(0o600)
	slog.SetDefault(slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: parseLevel(os.Getenv("MCPCTL_LOG_LEVEL"))})))
	logFile = f
	closer = closeLogger{}
	return closer, nil
}

func logDir() (string, error) {
	if dir := os.Getenv("MCPCTL_LOG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "mcpctl"), nil
}

func parseLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func rotate(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) || err != nil || info.Size() < maxBytes {
		return nil
	}
	for i := backupCount - 1; i >= 1; i-- {
		old := path + "." + strconv.Itoa(i)
		if _, err := os.Stat(old); err == nil {
			if err := os.Rename(old, path+"."+strconv.Itoa(i+1)); err != nil {
				return err
			}
		}
	}
	return os.Rename(path, path+".1")
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

type closeLogger struct{}

func (closeLogger) Close() error {
	mu.Lock()
	defer mu.Unlock()
	if logFile == nil {
		return nil
	}
	err := logFile.Close()
	logFile = nil
	closer = nil
	return err
}

// CommandName returns a compact command description for logs.
func CommandName(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := []string{filepath.Base(argv[0])}
	redactNext := false
	for _, arg := range argv[1:] {
		if redactNext {
			parts = append(parts, "<redacted>")
			redactNext = false
			continue
		}
		lower := strings.ToLower(arg)
		if lower == "-c" || lower == "--config" || lower == "--header" || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			if strings.Contains(arg, "=") {
				parts = append(parts, strings.SplitN(arg, "=", 2)[0]+"=<redacted>")
			} else {
				parts = append(parts, arg)
				redactNext = true
			}
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

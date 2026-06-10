package debuglog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

var mu sync.Mutex

func Authf(component, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()

	path := logPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()

	line := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(
		file,
		"%s [%s] pid=%d %s\n",
		time.Now().UTC().Format(time.RFC3339Nano),
		component,
		os.Getpid(),
		strings.TrimSpace(line),
	)
}

func logPath() string {
	if override := strings.TrimSpace(os.Getenv("CLOINK_AUTH_DEBUG_PATH")); override != "" {
		return override
	}
	if runtime.GOOS == "windows" {
		if programData := strings.TrimSpace(os.Getenv("ProgramData")); programData != "" {
			return filepath.Join(programData, "Cloink", "auth-debug.log")
		}
	}
	return filepath.Join(os.TempDir(), "cloink-auth-debug.log")
}

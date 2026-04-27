package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/netbirdio/netbird/client/internal/netflow/types"
)

type File struct {
	mux           sync.Mutex
	basePath      string
	maxSizeMB     int
	maxFiles      int
	currentFile   *os.File
	currentSize   int64
	eventsInFile  int
}

func NewFileStore(basePath string, maxSizeMB, maxFiles int) *File {
	return &File{
		basePath:  basePath,
		maxSizeMB: maxSizeMB,
		maxFiles:  maxFiles,
	}
}

func (f *File) StoreEvent(event *types.Event) {
	f.mux.Lock()
	defer f.mux.Unlock()

	if err := f.ensureCurrentFile(); err != nil {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	line := fmt.Sprintf("%s\n", string(data))
	n, err := f.currentFile.WriteString(line)
	if err != nil {
		return
	}

	f.currentSize += int64(n)
	f.eventsInFile++

	if f.currentSize >= int64(f.maxSizeMB)*1024*1024 {
		f.rotate()
	}
}

func (f *File) ensureCurrentFile() error {
	if f.currentFile != nil {
		return nil
	}

	if f.basePath == "" {
		f.basePath = filepath.Join(os.TempDir(), "netbird-flows")
	}

	if err := os.MkdirAll(f.basePath, 0755); err != nil {
		return err
	}

	filename := fmt.Sprintf("flows-%s.log", time.Now().Format("20060102-150405"))
	filepath := filepath.Join(f.basePath, filename)

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	f.currentFile = file
	f.currentSize = 0
	f.eventsInFile = 0

	f.cleanupOldFiles()

	return nil
}

func (f *File) rotate() {
	if f.currentFile == nil {
		return
	}

	f.currentFile.Close()
	f.currentFile = nil
	f.currentSize = 0
	f.eventsInFile = 0

	f.ensureCurrentFile()
}

func (f *File) cleanupOldFiles() {
	files, err := filepath.Glob(filepath.Join(f.basePath, "flows-*.log"))
	if err != nil || len(files) <= f.maxFiles {
		return
	}

	sort.Strings(files)
	for i := 0; i < len(files)-f.maxFiles; i++ {
		os.Remove(files[i])
	}
}

func (f *File) Close() {
	f.mux.Lock()
	defer f.mux.Unlock()

	if f.currentFile != nil {
		f.currentFile.Close()
		f.currentFile = nil
	}
}

func (f *File) GetEvents() []*types.Event {
	// File store is for archival, not for active retrieval
	return nil
}

func (f *File) DeleteEvents(ids []uuid.UUID) {
	// File store doesn't support individual event deletion
}

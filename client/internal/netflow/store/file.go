package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/netbirdio/netbird/client/configs"
	"github.com/netbirdio/netbird/client/internal/netflow/types"
)

const flowFilePattern = "flows-*.log"

type File struct {
	mux          sync.Mutex
	basePath     string
	maxSizeMB    int
	maxFiles     int
	currentFile  *os.File
	currentSize  int64
	eventsInFile int
}

func NewFileStore(basePath string, maxSizeMB, maxFiles int) *File {
	return &File{
		basePath:  basePath,
		maxSizeMB: maxSizeMB,
		maxFiles:  maxFiles,
	}
}

func (f *File) Matches(basePath string, maxSizeMB, maxFiles int) bool {
	return normalizedFlowPath(f.basePath) == normalizedFlowPath(basePath) &&
		f.maxSizeMB == maxSizeMB &&
		f.maxFiles == maxFiles
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

	f.basePath = normalizedFlowPath(f.basePath)

	if err := os.MkdirAll(f.basePath, 0755); err != nil {
		return err
	}

	filename := fmt.Sprintf("flows-%s.log", time.Now().Format("20060102-150405"))
	filepath := filepath.Join(f.basePath, filename)

	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	size := int64(0)
	if info, err := file.Stat(); err == nil {
		size = info.Size()
	}

	f.currentFile = file
	f.currentSize = size
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
	files, err := filepath.Glob(filepath.Join(f.basePath, flowFilePattern))
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
	f.mux.Lock()
	defer f.mux.Unlock()

	files, err := f.flowFiles()
	if err != nil {
		return nil
	}

	events := make([]*types.Event, 0)
	for _, file := range files {
		fileEvents := readEvents(file)
		events = append(events, fileEvents...)
	}

	return events
}

func normalizedFlowPath(basePath string) string {
	if basePath == "" {
		if configs.StateDir != "" {
			return filepath.Join(configs.StateDir, "flows")
		}
		return filepath.Join(os.TempDir(), "netbird-flows")
	}
	return basePath
}

func (f *File) DeleteEvents(ids []uuid.UUID) {
	if len(ids) == 0 {
		return
	}

	f.mux.Lock()
	defer f.mux.Unlock()

	if f.currentFile != nil {
		f.currentFile.Close()
		f.currentFile = nil
	}

	deleted := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		deleted[id] = struct{}{}
	}

	files, err := f.flowFiles()
	if err != nil {
		return
	}

	for _, file := range files {
		f.rewriteWithout(file, deleted)
	}
}

func (f *File) flowFiles() ([]string, error) {
	f.basePath = normalizedFlowPath(f.basePath)

	files, err := filepath.Glob(filepath.Join(f.basePath, flowFilePattern))
	if err != nil {
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func (f *File) rewriteWithout(file string, deleted map[uuid.UUID]struct{}) {
	events := readEvents(file)
	if len(events) == 0 {
		os.Remove(file)
		return
	}

	remaining := make([]*types.Event, 0, len(events))
	for _, event := range events {
		if _, ok := deleted[event.ID]; ok {
			continue
		}
		remaining = append(remaining, event)
	}

	if len(remaining) == 0 {
		os.Remove(file)
		return
	}

	tmpFile := file + ".tmp"
	out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return
	}

	for _, event := range remaining {
		data, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s\n", data); err != nil {
			out.Close()
			os.Remove(tmpFile)
			return
		}
	}

	if err := out.Close(); err != nil {
		os.Remove(tmpFile)
		return
	}

	if err := os.Rename(tmpFile, file); err != nil {
		os.Remove(tmpFile)
	}
}

func readEvents(file string) []*types.Event {
	in, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer in.Close()

	reader := bufio.NewReader(in)
	events := make([]*types.Event, 0)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event types.Event
			if json.Unmarshal(line, &event) == nil {
				events = append(events, &event)
			}
		}

		if err != nil {
			if err != io.EOF {
				return events
			}
			break
		}
	}

	return events
}

package version_releases

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/management/server/account"
	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

type PlatformType string

const (
	PlatformTypeMacOS    PlatformType = "macos"
	PlatformTypeWindows  PlatformType = "windows"
	PlatformTypeLinux    PlatformType = "linux"
	PlatformTypeAndroid  PlatformType = "android"
)

type VersionRelease struct {
	ID          string       `json:"id"`
	Version     string       `json:"version"`
	Platform    PlatformType `json:"platform"`
	DownloadURL string       `json:"downloadUrl"`
	Description string       `json:"description,omitempty"`
	IsLatest    bool         `json:"isLatest,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
}

type FileInfo struct {
	Content []byte
	Name    string
}

type PersistedData struct {
	Versions   map[string]*VersionRelease `json:"versions"`
	AccountMap map[string][]string        `json:"accountMap"`
}

const (
	versionsDir    = "version_releases"
	dataFileName   = "versions.json"
	filesDir       = "files"
)

var (
	mu         sync.RWMutex
	versions   = make(map[string]*VersionRelease)
	accountMap = make(map[string][]string)
	fileStorage = make(map[string]*FileInfo)
	fileLock    = &sync.RWMutex{}
	dataDir     string
)

type handler struct {
	accountManager account.Manager
}

func AddEndpoints(accountManager account.Manager, router *mux.Router, rootRouter *mux.Router) {
	dataDir = "/var/lib/netbird"
	initStorage()

	h := newHandler(accountManager)
	router.HandleFunc("/version-releases", h.getAll).Methods("GET", "OPTIONS")
	router.HandleFunc("/version-releases", h.create).Methods("POST", "OPTIONS")
	router.HandleFunc("/version-releases/upload", h.uploadFile).Methods("POST", "OPTIONS")
	router.HandleFunc("/version-releases/{id}", h.get).Methods("GET", "OPTIONS")
	router.HandleFunc("/version-releases/{id}", h.update).Methods("PUT", "OPTIONS")
	router.HandleFunc("/version-releases/{id}", h.delete).Methods("DELETE", "OPTIONS")

	prefix := "/api"
	rootRouter.HandleFunc(prefix+"/version-releases/files/{id}", h.downloadFile).Methods("GET", "HEAD", "OPTIONS")
}

func initStorage() {
	dir := filepath.Join(dataDir, versionsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Errorf("failed to create version releases dir: %v", err)
		return
	}
	filesPath := filepath.Join(dir, filesDir)
	if err := os.MkdirAll(filesPath, 0755); err != nil {
		log.Errorf("failed to create version releases files dir: %v", err)
		return
	}
	loadData()
}

func loadData() {
	dataPath := filepath.Join(dataDir, versionsDir, dataFileName)
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Errorf("failed to read version releases data: %v", err)
		}
		return
	}
	var persisted PersistedData
	if err := json.Unmarshal(raw, &persisted); err != nil {
		log.Errorf("failed to parse version releases data: %v", err)
		return
	}
	mu.Lock()
	if persisted.Versions != nil {
		versions = persisted.Versions
	}
	if persisted.AccountMap != nil {
		accountMap = persisted.AccountMap
	}
	mu.Unlock()
	log.Infof("loaded %d version releases from disk", len(versions))
}

func saveData() {
	persisted := PersistedData{
		Versions:   versions,
		AccountMap: accountMap,
	}
	raw, err := json.Marshal(persisted)
	if err != nil {
		log.Errorf("failed to marshal version releases data: %v", err)
		return
	}
	dataPath := filepath.Join(dataDir, versionsDir, dataFileName)
	if err := os.WriteFile(dataPath, raw, 0644); err != nil {
		log.Errorf("failed to write version releases data: %v", err)
	}
}

func saveFileToDisk(fileID string, fi *FileInfo) {
	dir := filepath.Join(dataDir, versionsDir, filesDir)
	path := filepath.Join(dir, fileID)
	if err := os.WriteFile(path, fi.Content, 0644); err != nil {
		log.Errorf("failed to save file %s to disk: %v", fileID, err)
		return
	}
	metaPath := filepath.Join(dir, fileID+".meta")
	meta, _ := json.Marshal(map[string]string{"name": fi.Name})
	_ = os.WriteFile(metaPath, meta, 0644)
}

func loadFileFromDisk(fileID string) (*FileInfo, bool) {
	dir := filepath.Join(dataDir, versionsDir, filesDir)
	path := filepath.Join(dir, fileID)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	name := fileID
	metaPath := filepath.Join(dir, fileID+".meta")
	if meta, err := os.ReadFile(metaPath); err == nil {
		var m map[string]string
		if json.Unmarshal(meta, &m) == nil {
			if n, ok := m["name"]; ok {
				name = n
			}
		}
	}
	return &FileInfo{Content: content, Name: name}, true
}

func deleteFileFromDisk(fileID string) {
	dir := filepath.Join(dataDir, versionsDir, filesDir)
	os.Remove(filepath.Join(dir, fileID))
	os.Remove(filepath.Join(dir, fileID+".meta"))
}

func newHandler(accountManager account.Manager) *handler {
	return &handler{
		accountManager: accountManager,
	}
}

type CreateRequest struct {
	Version     string       `json:"version"`
	Platform    PlatformType `json:"platform"`
	DownloadURL string       `json:"downloadUrl"`
	Description string       `json:"description,omitempty"`
	IsLatest    bool         `json:"isLatest,omitempty"`
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID := userAuth.AccountId

	req := &CreateRequest{}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	if req.Version == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "version shouldn't be empty"), w)
		return
	}

	if req.DownloadURL == "" {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "downloadUrl shouldn't be empty"), w)
		return
	}

	if req.Platform != PlatformTypeMacOS && req.Platform != PlatformTypeWindows &&
		req.Platform != PlatformTypeLinux && req.Platform != PlatformTypeAndroid {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid platform type"), w)
		return
	}

	id := uuid.New().String()

	version := &VersionRelease{
		ID:          id,
		Version:     req.Version,
		Platform:    req.Platform,
		DownloadURL: req.DownloadURL,
		Description: req.Description,
		IsLatest:    req.IsLatest,
		CreatedAt:   time.Now(),
	}

	mu.Lock()
	if req.IsLatest {
		for _, v := range versions {
			if v.Platform == req.Platform {
				v.IsLatest = false
			}
		}
	}
	versions[id] = version
	accountMap[accountID] = append(accountMap[accountID], id)
	mu.Unlock()

	saveData()

	util.WriteJSONObject(r.Context(), w, version)
}

func (h *handler) getAll(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	accountID := userAuth.AccountId

	mu.RLock()
	defer mu.RUnlock()

	var result []*VersionRelease
	for _, id := range accountMap[accountID] {
		if v, ok := versions[id]; ok {
			result = append(result, v)
		}
	}

	util.WriteJSONObject(r.Context(), w, result)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	_, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]
	if len(id) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "id shouldn't be empty"), w)
		return
	}

	mu.RLock()
	version, ok := versions[id]
	mu.RUnlock()

	if !ok {
		util.WriteErrorResponse("version not found", http.StatusNotFound, w)
		return
	}

	util.WriteJSONObject(r.Context(), w, version)
}

type UpdateRequest struct {
	Version     string       `json:"version"`
	Platform    PlatformType `json:"platform"`
	DownloadURL string       `json:"downloadUrl"`
	Description string       `json:"description,omitempty"`
	IsLatest    bool         `json:"isLatest,omitempty"`
}

func (h *handler) update(w http.ResponseWriter, r *http.Request) {
	_, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]
	if len(id) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "id shouldn't be empty"), w)
		return
	}

	req := &UpdateRequest{}
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	version, ok := versions[id]
	if !ok {
		util.WriteErrorResponse("version not found", http.StatusNotFound, w)
		return
	}

	if req.IsLatest {
		for _, v := range versions {
			if v.Platform == req.Platform {
				v.IsLatest = false
			}
		}
	}

	version.Version = req.Version
	version.Platform = req.Platform
	version.DownloadURL = req.DownloadURL
	version.Description = req.Description
	version.IsLatest = req.IsLatest

	util.WriteJSONObject(r.Context(), w, version)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]
	if len(id) == 0 {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "id shouldn't be empty"), w)
		return
	}

	accountID := userAuth.AccountId

	mu.Lock()
	defer mu.Unlock()

	version, ok := versions[id]
	if !ok {
		util.WriteErrorResponse("version not found", http.StatusNotFound, w)
		return
	}

	// if there's a file URL, delete the file from disk
	if version.DownloadURL != "" {
		for fileID := range fileStorage {
			if version.DownloadURL == "/api/version-releases/files/"+fileID || 
			   version.DownloadURL == dataDir+"/"+fileID {
				deleteFileFromDisk(fileID)
				delete(fileStorage, fileID)
				break
			}
		}
	}

	delete(versions, id)
	for i, v := range accountMap[accountID] {
		if v == id {
			accountMap[accountID] = append(accountMap[accountID][:i], accountMap[accountID][i+1:]...)
			break
		}
	}

	saveData()

	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

// uploadFile handles file uploads
func (h *handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	_, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}

	// parse multipart form
	err = r.ParseMultipartForm(32 << 20) // 32MB
	if err != nil {
		util.WriteErrorResponse("couldn't parse form", http.StatusBadRequest, w)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		util.WriteErrorResponse("couldn't read file", http.StatusBadRequest, w)
		return
	}
	defer file.Close()

	// read file
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		util.WriteErrorResponse("couldn't read file contents", http.StatusInternalServerError, w)
		return
	}

	// store file
	fileID := uuid.New().String()
	fi := &FileInfo{
		Content: fileBytes,
		Name:    handler.Filename,
	}

	fileLock.Lock()
	fileStorage[fileID] = fi
	fileLock.Unlock()

	saveFileToDisk(fileID, fi)
	saveData()

	util.WriteJSONObject(r.Context(), w, map[string]interface{}{
		"id":       fileID,
		"filename": handler.Filename,
		"size":     len(fileBytes),
	})
}

// downloadFile serves uploaded files - allows public access without authentication
func (h *handler) downloadFile(w http.ResponseWriter, r *http.Request) {
	// Handle CORS preflight OPTIONS request
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}
	
	vars := mux.Vars(r)
	fileID := vars["id"]
	if len(fileID) == 0 {
		util.WriteErrorResponse("file id shouldn't be empty", http.StatusBadRequest, w)
		return
	}

	fileLock.RLock()
	fileInfo, ok := fileStorage[fileID]
	fileLock.RUnlock()

	if !ok {
		fileInfo, ok = loadFileFromDisk(fileID)
		if !ok {
			util.WriteErrorResponse("file not found", http.StatusNotFound, w)
			return
		}
		fileLock.Lock()
		fileStorage[fileID] = fileInfo
		fileLock.Unlock()
	}

	// set response headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+fileInfo.Name+"\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(fileInfo.Content)))

	if r.Method == "HEAD" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(fileInfo.Content)
}

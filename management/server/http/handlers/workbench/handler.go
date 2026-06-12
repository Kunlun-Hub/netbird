package workbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/xid"

	nbcontext "github.com/netbirdio/netbird/management/server/context"
	workbenchManager "github.com/netbirdio/netbird/management/server/workbench"
	"github.com/netbirdio/netbird/management/server/workbench/types"
	"github.com/netbirdio/netbird/shared/management/http/util"
	"github.com/netbirdio/netbird/shared/management/status"
)

const (
	maxIconSize        = 1024 * 1024
	maxIconRequestSize = maxIconSize + 64*1024
	maxJSONRequestSize = 256 * 1024
	assetURLPattern    = "/api/workbench/assets/%s"
)

var iconTagPattern = regexp.MustCompile(`(?is)<(?:link|meta)\b[^>]*(?:rel|property)=["'][^"']*(?:icon|apple-touch-icon|og:image)[^"']*["'][^>]*(?:href|content)=["']([^"']+)["']|<(?:link|meta)\b[^>]*(?:href|content)=["']([^"']+)["'][^>]*(?:rel|property)=["'][^"']*(?:icon|apple-touch-icon|og:image)[^"']*["'][^>]*`)
var assetsDir = "/var/lib/netbird/workbench/assets"
var disallowedIconAddrPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type webManifest struct {
	Icons []struct {
		Src string `json:"src"`
	} `json:"icons"`
}

type handler struct {
	manager workbenchManager.Manager
}

func SetAssetsDirFromDataDir(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return
	}
	assetsDir = filepath.Join(dataDir, "workbench", "assets")
}

func AddEndpoints(manager workbenchManager.Manager, router *mux.Router) {
	h := &handler{manager: manager}

	router.HandleFunc("/workbench/resources", h.listResources).Methods(http.MethodGet, "OPTIONS")
	router.HandleFunc("/workbench/resources/{resourceId:.*}/launch", h.recordLaunch).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/personal/resources", h.createPersonalResource).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/personal/resources/{resourceId:.*}", h.updatePersonalResource).Methods(http.MethodPut, "OPTIONS")
	router.HandleFunc("/workbench/personal/resources/{resourceId:.*}", h.deletePersonalResource).Methods(http.MethodDelete, "OPTIONS")
	router.HandleFunc("/workbench/personal/assets/icon", h.uploadPersonalIcon).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/personal/assets/fetch-icon", h.fetchPersonalIcon).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/assets/{assetId}", h.getAsset).Methods(http.MethodGet, "OPTIONS")

	router.HandleFunc("/workbench/admin/resources", h.listAdminResources).Methods(http.MethodGet, "OPTIONS")
	router.HandleFunc("/workbench/admin/resources", h.createAdminResource).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/admin/resources/{resourceId:.*}", h.getAdminResource).Methods(http.MethodGet, "OPTIONS")
	router.HandleFunc("/workbench/admin/resources/{resourceId:.*}", h.updateAdminResource).Methods(http.MethodPut, "OPTIONS")
	router.HandleFunc("/workbench/admin/resources/{resourceId:.*}", h.deleteAdminResource).Methods(http.MethodDelete, "OPTIONS")
	router.HandleFunc("/workbench/admin/categories", h.listAdminCategories).Methods(http.MethodGet, "OPTIONS")
	router.HandleFunc("/workbench/admin/categories", h.createAdminCategory).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/admin/categories/{categoryId:.*}", h.updateAdminCategory).Methods(http.MethodPut, "OPTIONS")
	router.HandleFunc("/workbench/admin/categories/{categoryId:.*}", h.deleteAdminCategory).Methods(http.MethodDelete, "OPTIONS")
	router.HandleFunc("/workbench/admin/assets/icon", h.uploadAdminIcon).Methods(http.MethodPost, "OPTIONS")
	router.HandleFunc("/workbench/admin/assets/fetch-icon", h.fetchAdminIcon).Methods(http.MethodPost, "OPTIONS")
}

func (h *handler) listResources(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resources, err := h.manager.ListResources(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resources)
}

type launchRequest struct {
	Scope string `json:"scope"`
}

func (h *handler) recordLaunch(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	defer r.Body.Close()

	var body launchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		util.WriteError(r.Context(), status.Errorf(status.InvalidArgument, "invalid request body"), w)
		return
	}
	if err := h.manager.RecordLaunch(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID, body.Scope); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func (h *handler) listAdminResources(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resources, err := h.manager.ListAdminResources(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resources)
}

func (h *handler) getAdminResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resource, err := h.manager.GetAdminResource(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, resource)
}

func (h *handler) createAdminResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resource, ok := decodeResource(w, r)
	if !ok {
		return
	}
	created, err := h.manager.CreateAdminResource(r.Context(), userAuth.AccountId, userAuth.UserId, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, created)
}

func (h *handler) updateAdminResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resource, ok := decodeResource(w, r)
	if !ok {
		return
	}
	updated, err := h.manager.UpdateAdminResource(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, updated)
}

func (h *handler) deleteAdminResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.DeleteAdminResource(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func (h *handler) listAdminCategories(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	categories, err := h.manager.ListAdminCategories(r.Context(), userAuth.AccountId, userAuth.UserId)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, categories)
}

func (h *handler) createAdminCategory(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	category, ok := decodeCategory(w, r)
	if !ok {
		return
	}
	created, err := h.manager.CreateAdminCategory(r.Context(), userAuth.AccountId, userAuth.UserId, category)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, created)
}

func (h *handler) updateAdminCategory(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	categoryID, err := categoryIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	category, ok := decodeCategory(w, r)
	if !ok {
		return
	}
	updated, err := h.manager.UpdateAdminCategory(r.Context(), userAuth.AccountId, userAuth.UserId, categoryID, category)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, updated)
}

func (h *handler) deleteAdminCategory(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	categoryID, err := categoryIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.DeleteAdminCategory(r.Context(), userAuth.AccountId, userAuth.UserId, categoryID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func (h *handler) createPersonalResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resource, ok := decodeResource(w, r)
	if !ok {
		return
	}
	created, err := h.manager.CreatePersonalResource(r.Context(), userAuth.AccountId, userAuth.UserId, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, created)
}

func (h *handler) updatePersonalResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resource, ok := decodeResource(w, r)
	if !ok {
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	updated, err := h.manager.UpdatePersonalResource(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID, resource)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, updated)
}

func (h *handler) deletePersonalResource(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	resourceID, err := resourceIDFromRequest(r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.DeletePersonalResource(r.Context(), userAuth.AccountId, userAuth.UserId, resourceID); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, util.EmptyObject{})
}

func (h *handler) uploadPersonalIcon(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.EnsurePersonalAssetAccess(r.Context(), userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	content, contentType, err := parseUploadedIcon(w, r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.savePersonalIconResponse(w, r, userAuth.AccountId, userAuth.UserId, "", content, contentType, types.IconModeUploaded)
}

func (h *handler) fetchPersonalIcon(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.EnsurePersonalAssetAccess(r.Context(), userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	content, contentType, sourceURL, err := fetchIcon(r, req.URL)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.savePersonalIconResponse(w, r, userAuth.AccountId, userAuth.UserId, sourceURL, content, contentType, types.IconModeFetched)
}

func (h *handler) uploadAdminIcon(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.EnsureAdminAssetAccess(r.Context(), userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	content, contentType, err := parseUploadedIcon(w, r)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.saveAdminIconResponse(w, r, userAuth.AccountId, userAuth.UserId, "", content, contentType, types.IconModeUploaded)
}

func (h *handler) fetchAdminIcon(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	if err := h.manager.EnsureAdminAssetAccess(r.Context(), userAuth.AccountId, userAuth.UserId); err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return
	}
	content, contentType, sourceURL, err := fetchIcon(r, req.URL)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	h.saveAdminIconResponse(w, r, userAuth.AccountId, userAuth.UserId, sourceURL, content, contentType, types.IconModeFetched)
}

func (h *handler) getAsset(w http.ResponseWriter, r *http.Request) {
	userAuth, err := nbcontext.GetUserAuthFromContext(r.Context())
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	asset, err := h.manager.GetAsset(r.Context(), userAuth.AccountId, userAuth.UserId, mux.Vars(r)["assetId"])
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	file, err := os.Open(asset.StoragePath)
	if err != nil {
		util.WriteError(r.Context(), status.Errorf(status.NotFound, "workbench asset file not found"), w)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", asset.ContentType)
	setAssetSecurityHeaders(w)
	http.ServeContent(w, r, filepath.Base(asset.StoragePath), asset.CreatedAt, file)
}

func (h *handler) savePersonalIconResponse(w http.ResponseWriter, r *http.Request, accountID, userID, sourceURL string, content []byte, contentType, iconMode string) {
	assetID := xid.New().String()
	storagePath, sha, err := saveIconFile(assetID, content)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	publicURL := "/api/workbench/assets/" + assetID
	if _, err := h.manager.SavePersonalIconAsset(r.Context(), accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha, int64(len(content))); err != nil {
		cleanupIconFile(storagePath)
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, &types.IconAssetResult{IconURL: publicURL, IconMode: iconMode})
}

func (h *handler) saveAdminIconResponse(w http.ResponseWriter, r *http.Request, accountID, userID, sourceURL string, content []byte, contentType, iconMode string) {
	assetID := xid.New().String()
	storagePath, sha, err := saveIconFile(assetID, content)
	if err != nil {
		util.WriteError(r.Context(), err, w)
		return
	}
	publicURL := "/api/workbench/assets/" + assetID
	if _, err := h.manager.SaveAdminIconAsset(r.Context(), accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha, int64(len(content))); err != nil {
		cleanupIconFile(storagePath)
		util.WriteError(r.Context(), err, w)
		return
	}
	util.WriteJSONObject(r.Context(), w, &types.IconAssetResult{IconURL: publicURL, IconMode: iconMode})
}

func decodeResource(w http.ResponseWriter, r *http.Request) (*types.Resource, bool) {
	resource := &types.Resource{}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	if err := json.NewDecoder(r.Body).Decode(resource); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return nil, false
	}
	return resource, true
}

func decodeCategory(w http.ResponseWriter, r *http.Request) (*types.Category, bool) {
	category := &types.Category{}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONRequestSize)
	if err := json.NewDecoder(r.Body).Decode(category); err != nil {
		util.WriteErrorResponse("couldn't parse JSON request", http.StatusBadRequest, w)
		return nil, false
	}
	return category, true
}

func setAssetSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'; sandbox")
}

func categoryIDFromRequest(r *http.Request) (string, error) {
	categoryID := mux.Vars(r)["categoryId"]
	decoded, err := url.PathUnescape(categoryID)
	if err != nil {
		return "", status.Errorf(status.InvalidArgument, "category id is invalid")
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", status.Errorf(status.InvalidArgument, "category id shouldn't be empty")
	}
	return decoded, nil
}

func resourceIDFromRequest(r *http.Request) (string, error) {
	resourceID := mux.Vars(r)["resourceId"]
	decoded, err := url.PathUnescape(resourceID)
	if err != nil {
		return "", status.Errorf(status.InvalidArgument, "resource id is invalid")
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", status.Errorf(status.InvalidArgument, "resource id shouldn't be empty")
	}
	return decoded, nil
}

func parseUploadedIcon(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxIconRequestSize)
	if err := r.ParseMultipartForm(maxIconSize); err != nil {
		return nil, "", status.Errorf(status.InvalidArgument, "invalid icon upload")
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, "", status.Errorf(status.InvalidArgument, "icon file is required")
	}
	defer file.Close()
	content, contentType, err := readIcon(file)
	if err != nil {
		return nil, "", err
	}
	if header != nil && header.Size > maxIconSize {
		return nil, "", status.Errorf(status.InvalidArgument, "icon file is too large")
	}
	return content, contentType, nil
}

func readIcon(reader io.Reader) ([]byte, string, error) {
	limited := io.LimitReader(reader, maxIconSize+1)
	content, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", status.Errorf(status.InvalidArgument, "failed to read icon")
	}
	if len(content) == 0 || len(content) > maxIconSize {
		return nil, "", status.Errorf(status.InvalidArgument, "icon file is invalid or too large")
	}
	if isSVGIcon(content) {
		return content, "image/svg+xml", nil
	}
	contentType := http.DetectContentType(content)
	if !isAllowedIconType(contentType) {
		return nil, "", status.Errorf(status.InvalidArgument, "unsupported icon type")
	}
	return content, contentType, nil
}

func fetchIcon(r *http.Request, rawURL string) ([]byte, string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", "", status.Errorf(status.InvalidArgument, "url is invalid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if parsed.Host == "" || (scheme != "http" && scheme != "https") {
		return nil, "", "", status.Errorf(status.InvalidArgument, "url is invalid")
	}
	if err := validatePublicIconURL(parsed); err != nil {
		return nil, "", "", err
	}
	client := newIconHTTPClient()
	return fetchIconFromURL(r, client, parsed)
}

func newIconHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, address)
				if err != nil {
					return nil, err
				}
				if err := validatePublicRemoteAddr(conn.RemoteAddr()); err != nil {
					_ = conn.Close()
					return nil, err
				}
				return conn, nil
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return status.Errorf(status.InvalidArgument, "too many icon redirects")
			}
			if err := validatePublicIconURL(req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

func validatePublicRemoteAddr(remote net.Addr) error {
	if remote == nil {
		return status.Errorf(status.InvalidArgument, "url host is not allowed")
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err != nil {
		host = remote.String()
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !isPublicIconAddr(addr) {
		return status.Errorf(status.InvalidArgument, "url host is not allowed")
	}
	return nil
}

func fetchIconFromURL(r *http.Request, client *http.Client, parsed *url.URL) ([]byte, string, string, error) {
	for _, iconURL := range iconCandidates(r, client, parsed) {
		content, contentType, err := fetchIconURL(r, client, iconURL)
		if err == nil {
			return content, contentType, iconURL, nil
		}
	}
	return nil, "", "", status.Errorf(status.NotFound, "icon not found")
}

func validatePublicIconURL(parsed *url.URL) error {
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return status.Errorf(status.InvalidArgument, "url is invalid")
	}
	normalizedHost := strings.TrimSuffix(strings.ToLower(host), ".")
	if normalizedHost == "localhost" || strings.HasSuffix(normalizedHost, ".localhost") {
		return status.Errorf(status.InvalidArgument, "url host is not allowed")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isPublicIconAddr(addr) {
			return status.Errorf(status.InvalidArgument, "url host is not allowed")
		}
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return status.Errorf(status.InvalidArgument, "url host is invalid")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok || !isPublicIconAddr(addr) {
			return status.Errorf(status.InvalidArgument, "url host is not allowed")
		}
	}
	return nil
}

func isPublicIconAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() ||
		!addr.IsGlobalUnicast() ||
		addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() {
		return false
	}
	for _, prefix := range disallowedIconAddrPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func fetchIconURL(r *http.Request, client *http.Client, iconURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, iconURL, nil)
	if err != nil {
		return nil, "", status.Errorf(status.InvalidArgument, "failed to fetch icon")
	}
	if err := validatePublicIconURL(req.URL); err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", status.Errorf(status.NotFound, "icon not found")
	}
	content, contentType, err := readIcon(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return content, contentType, nil
}

func iconCandidates(r *http.Request, client *http.Client, pageURL *url.URL) []string {
	candidates := []string{
		pageURL.ResolveReference(&url.URL{Path: "/favicon.ico"}).String(),
		pageURL.ResolveReference(&url.URL{Path: "/apple-touch-icon.png"}).String(),
		pageURL.ResolveReference(&url.URL{Path: "/apple-touch-icon-precomposed.png"}).String(),
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return dedupeStrings(candidates)
	}
	resp, err := client.Do(req)
	if err != nil {
		return dedupeStrings(candidates)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return dedupeStrings(candidates)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return dedupeStrings(candidates)
	}
	for _, match := range iconTagPattern.FindAllStringSubmatch(string(body), 12) {
		iconRef := strings.TrimSpace(firstNonEmpty(match[1], match[2]))
		if iconRef == "" {
			continue
		}
		parsedIcon, err := url.Parse(iconRef)
		if err != nil {
			continue
		}
		candidates = append(candidates, pageURL.ResolveReference(parsedIcon).String())
	}
	for _, manifestURL := range manifestCandidatesFromHTML(string(body), pageURL) {
		candidates = append(candidates, iconCandidatesFromManifest(r, client, manifestURL)...)
	}
	return dedupeStrings(candidates)
}

func manifestCandidatesFromHTML(body string, pageURL *url.URL) []string {
	var candidates []string
	matches := regexp.MustCompile(`(?is)<link\b[^>]*rel=["'][^"']*manifest[^"']*["'][^>]*href=["']([^"']+)["'][^>]*|<link\b[^>]*href=["']([^"']+)["'][^>]*rel=["'][^"']*manifest[^"']*["'][^>]*`).FindAllStringSubmatch(body, 4)
	for _, match := range matches {
		manifestRef := strings.TrimSpace(firstNonEmpty(match[1], match[2]))
		if manifestRef == "" {
			continue
		}
		parsedManifest, err := url.Parse(manifestRef)
		if err != nil {
			continue
		}
		candidates = append(candidates, pageURL.ResolveReference(parsedManifest).String())
	}
	return dedupeStrings(candidates)
}

func iconCandidatesFromManifest(r *http.Request, client *http.Client, manifestURL string) []string {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil
	}
	if err := validatePublicIconURL(req.URL); err != nil {
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	var manifest webManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256*1024)).Decode(&manifest); err != nil {
		return nil
	}
	baseURL := resp.Request.URL
	if baseURL == nil {
		baseURL = req.URL
	}
	candidates := make([]string, 0, len(manifest.Icons))
	for _, icon := range manifest.Icons {
		iconRef := strings.TrimSpace(icon.Src)
		if iconRef == "" {
			continue
		}
		parsedIcon, err := url.Parse(iconRef)
		if err != nil {
			continue
		}
		candidates = append(candidates, baseURL.ResolveReference(parsedIcon).String())
	}
	return dedupeStrings(candidates)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func saveIconFile(assetID string, content []byte) (string, string, error) {
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return "", "", status.Errorf(status.Internal, "failed to prepare icon storage")
	}
	if !isSafeAssetFileName(assetID) {
		return "", "", status.Errorf(status.InvalidArgument, "invalid workbench asset id")
	}
	shaBytes := sha256.Sum256(content)
	sha := hex.EncodeToString(shaBytes[:])
	path := filepath.Join(assetsDir, assetID)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return "", "", status.Errorf(status.Internal, "failed to save icon")
	}
	return path, sha, nil
}

func isSafeAssetFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return name == filepath.Base(name) && !strings.ContainsAny(name, `/\`)
}

func cleanupIconFile(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

func isAllowedIconType(contentType string) bool {
	if strings.HasPrefix(contentType, "image/png") ||
		strings.HasPrefix(contentType, "image/jpeg") ||
		strings.HasPrefix(contentType, "image/gif") ||
		strings.HasPrefix(contentType, "image/webp") ||
		strings.HasPrefix(contentType, "image/svg+xml") ||
		strings.HasPrefix(contentType, "image/x-icon") ||
		strings.HasPrefix(contentType, "image/vnd.microsoft.icon") {
		return true
	}
	if strings.HasPrefix(contentType, "text/plain") {
		return false
	}
	return bytes.HasPrefix([]byte(contentType), []byte("image/"))
}

func isSVGIcon(content []byte) bool {
	trimmed := strings.TrimSpace(string(content))
	if strings.HasPrefix(trimmed, "<?xml") {
		if end := strings.Index(trimmed, "?>"); end >= 0 {
			trimmed = strings.TrimSpace(trimmed[end+2:])
		}
	}
	return strings.HasPrefix(strings.ToLower(trimmed), "<svg")
}

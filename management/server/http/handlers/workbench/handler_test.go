package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/store"
	nbtypes "github.com/netbirdio/netbird/management/server/types"
	workbenchManager "github.com/netbirdio/netbird/management/server/workbench"
	"github.com/netbirdio/netbird/management/server/workbench/types"
	"github.com/netbirdio/netbird/shared/auth"
	"github.com/netbirdio/netbird/shared/management/status"
)

func TestListResourcesEndpointUsesAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{
		listResources: &types.ResourceList{
			ServerResources: []types.Resource{{
				ID:      "server-resource",
				Name:    "Server Resource",
				URL:     "https://server.example.com",
				Enabled: true,
				Scope:   types.ResourceScopeServer,
			}},
			PersonalResources: []types.Resource{{
				ID:      "personal-resource",
				Name:    "Personal Resource",
				URL:     "https://personal.example.com",
				Enabled: true,
				Scope:   types.ResourceScopePersonal,
			}},
			Version: 1,
		},
	}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/resources", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "user-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 user-1", manager.lastAccountID, manager.lastUserID)
	}
	var response types.ResourceList
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got, want := response.ServerResources[0].ID, "server-resource"; got != want {
		t.Fatalf("server resource ID = %q, want %q", got, want)
	}
	if got, want := response.PersonalResources[0].ID, "personal-resource"; got != want {
		t.Fatalf("personal resource ID = %q, want %q", got, want)
	}
}

func TestResourcesEndpointRequiresAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{
		listResources: &types.ResourceList{Version: 1},
	}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/resources", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want auth context rejection", recorder.Code)
	}
	if manager.lastAccountID != "" || manager.lastUserID != "" {
		t.Fatalf("manager called with account %q user %q, want no manager call without auth", manager.lastAccountID, manager.lastUserID)
	}
}

func TestRecordLaunchEndpointUsesAuthContextAndPathID(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodPost, "/workbench/resources/server-1/launch", strings.NewReader(`{"scope":"server"}`))
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "user-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 user-1", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "server-1" {
		t.Fatalf("resource ID = %q, want server-1", manager.lastResourceID)
	}
	if manager.lastScope != "server" {
		t.Fatalf("scope = %q, want server", manager.lastScope)
	}
}

func TestCreatePersonalResourceEndpointDecodesResource(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body := strings.NewReader(`{"name":"我的系统","url":"https://example.com","category":"个人资源","tags":["常用"]}`)
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/resources", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-2", UserId: "user-2"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-2" || manager.lastUserID != "user-2" {
		t.Fatalf("manager auth = account %q user %q, want account-2 user-2", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResource == nil {
		t.Fatal("manager did not receive resource")
	}
	if got, want := manager.lastResource.Name, "我的系统"; got != want {
		t.Fatalf("resource name = %q, want %q", got, want)
	}
	if got, want := manager.lastResource.URL, "https://example.com"; got != want {
		t.Fatalf("resource URL = %q, want %q", got, want)
	}
	var response types.Resource
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got, want := response.Scope, types.ResourceScopePersonal; got != want {
		t.Fatalf("response scope = %q, want %q", got, want)
	}
}

func TestCreatePersonalResourceEndpointRejectsOversizedJSONBody(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body := strings.NewReader(`{"name":"` + strings.Repeat("x", maxJSONRequestSize+1) + `","url":"https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/resources", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-2", UserId: "user-2"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want oversized JSON rejection", recorder.Code)
	}
	if manager.lastResource != nil {
		t.Fatalf("manager received resource despite oversized JSON: %+v", manager.lastResource)
	}
}

func TestUpdatePersonalResourceEndpointUsesPathIDAndAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body := strings.NewReader(`{"name":"我的系统","url":"https://updated.example.com","category":"个人资源"}`)
	req := httptest.NewRequest(http.MethodPut, "/workbench/personal/resources/personal-1", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-3", UserId: "user-3"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-3" || manager.lastUserID != "user-3" {
		t.Fatalf("manager auth = account %q user %q, want account-3 user-3", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "personal-1" {
		t.Fatalf("resource ID = %q, want personal-1", manager.lastResourceID)
	}
	if manager.lastResource == nil || manager.lastResource.URL != "https://updated.example.com" {
		t.Fatalf("manager resource = %+v, want updated URL", manager.lastResource)
	}
	var response types.Resource
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.ID != "personal-1" || response.Scope != types.ResourceScopePersonal {
		t.Fatalf("response = %+v, want path ID and personal scope", response)
	}
}

func TestDeletePersonalResourceEndpointUsesPathIDAndAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodDelete, "/workbench/personal/resources/personal-2", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-4", UserId: "user-4"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-4" || manager.lastUserID != "user-4" {
		t.Fatalf("manager auth = account %q user %q, want account-4 user-4", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "personal-2" {
		t.Fatalf("resource ID = %q, want personal-2", manager.lastResourceID)
	}
}

func TestPersonalResourceEndpointAcceptsEscapedSlashInResourceID(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodDelete, "/workbench/personal/resources/personal%2F1", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-4", UserId: "user-4"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastResourceID != "personal/1" {
		t.Fatalf("resource ID = %q, want personal/1", manager.lastResourceID)
	}
}

func TestPersonalResourceEndpointsRejectEmptyResourceID(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/workbench/personal/resources/",
			body:   strings.NewReader(`{"name":"个人系统","url":"https://example.com"}`),
		},
		{
			name:   "delete",
			method: http.MethodDelete,
			path:   "/workbench/personal/resources/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeWorkbenchManager{}
			router := mux.NewRouter()
			AddEndpoints(manager, router)

			req := httptest.NewRequest(tt.method, tt.path, tt.body)
			req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-4", UserId: "user-4"})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				t.Fatalf("status = %d, want empty resource ID rejection", recorder.Code)
			}
			if manager.lastResourceID != "" || manager.lastAccountID != "" || manager.lastUserID != "" {
				t.Fatalf("manager called with account=%q user=%q resource=%q, want no manager call", manager.lastAccountID, manager.lastUserID, manager.lastResourceID)
			}
		})
	}
}

func TestUpdateAdminResourceEndpointUsesPathIDAndAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body := strings.NewReader(`{"name":"企业门户","url":"https://portal.example.com","category":"常用应用"}`)
	req := httptest.NewRequest(http.MethodPut, "/workbench/admin/resources/server-1", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-5", UserId: "admin-5"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-5" || manager.lastUserID != "admin-5" {
		t.Fatalf("manager auth = account %q user %q, want account-5 admin-5", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "server-1" {
		t.Fatalf("resource ID = %q, want server-1", manager.lastResourceID)
	}
	if manager.lastResource == nil || manager.lastResource.URL != "https://portal.example.com" {
		t.Fatalf("manager resource = %+v, want portal URL", manager.lastResource)
	}
}

func TestDeleteAdminResourceEndpointUsesPathIDAndAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodDelete, "/workbench/admin/resources/server-2", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-6", UserId: "admin-6"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-6" || manager.lastUserID != "admin-6" {
		t.Fatalf("manager auth = account %q user %q, want account-6 admin-6", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "server-2" {
		t.Fatalf("resource ID = %q, want server-2", manager.lastResourceID)
	}
}

func TestGetAdminResourceEndpointUsesPathIDAndAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/admin/resources/server-3", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-7", UserId: "admin-7"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-7" || manager.lastUserID != "admin-7" {
		t.Fatalf("manager auth = account %q user %q, want account-7 admin-7", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResourceID != "server-3" {
		t.Fatalf("resource ID = %q, want server-3", manager.lastResourceID)
	}
	var response types.Resource
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.ID != "server-3" || response.Scope != types.ResourceScopeServer {
		t.Fatalf("response = %+v, want path ID and server scope", response)
	}
}

func TestCreateAdminResourceEndpointDecodesResourceAndUsesAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body := strings.NewReader(`{"name":"企业门户","url":"https://portal.example.com","category":"常用应用","visibility":"all"}`)
	req := httptest.NewRequest(http.MethodPost, "/workbench/admin/resources", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-8", UserId: "admin-8"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-8" || manager.lastUserID != "admin-8" {
		t.Fatalf("manager auth = account %q user %q, want account-8 admin-8", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastResource == nil || manager.lastResource.Name != "企业门户" || manager.lastResource.URL != "https://portal.example.com" {
		t.Fatalf("manager resource = %+v, want decoded admin resource", manager.lastResource)
	}
	var response types.Resource
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Scope != types.ResourceScopeServer {
		t.Fatalf("response scope = %q, want server", response.Scope)
	}
}

func TestAdminResourceEndpointAcceptsEscapedSlashInResourceID(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/admin/resources/server%2F1", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-6", UserId: "admin-6"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastResourceID != "server/1" {
		t.Fatalf("resource ID = %q, want server/1", manager.lastResourceID)
	}
}

func TestAdminResourceEndpointsRejectEmptyResourceID(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{
			name:   "get",
			method: http.MethodGet,
			path:   "/workbench/admin/resources/",
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/workbench/admin/resources/",
			body:   strings.NewReader(`{"name":"企业门户","url":"https://example.com"}`),
		},
		{
			name:   "delete",
			method: http.MethodDelete,
			path:   "/workbench/admin/resources/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeWorkbenchManager{}
			router := mux.NewRouter()
			AddEndpoints(manager, router)

			req := httptest.NewRequest(tt.method, tt.path, tt.body)
			req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-7", UserId: "admin-7"})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				t.Fatalf("status = %d, want empty resource ID rejection", recorder.Code)
			}
			if manager.lastResourceID != "" || manager.lastAccountID != "" || manager.lastUserID != "" {
				t.Fatalf("manager called with account=%q user=%q resource=%q, want no manager call", manager.lastAccountID, manager.lastUserID, manager.lastResourceID)
			}
		})
	}
}

func TestListAdminResourcesEndpointUsesAuthContext(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/admin/resources", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-9", UserId: "admin-9"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-9" || manager.lastUserID != "admin-9" {
		t.Fatalf("manager auth = account %q user %q, want account-9 admin-9", manager.lastAccountID, manager.lastUserID)
	}
	var response []types.Resource
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(response) != 1 || response[0].ID != "server-list-resource" {
		t.Fatalf("response = %+v, want server-list-resource", response)
	}
}

func TestResourcesEndpointWithManagerFiltersServerResourcesAndPersistsPersonalResources(t *testing.T) {
	accountID := "account-1"
	userID := "user-1"
	memoryStore := &httpWorkbenchStore{
		user: &nbtypes.User{
			Id:         userID,
			AccountID:  accountID,
			UserGroups: []string{"group-1"},
		},
		serverResources: []*types.Resource{
			{
				ID:         "all-resource",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "全部可见",
				URL:        "https://all.example.com",
				Enabled:    true,
				Visibility: types.VisibilityAll,
			},
			{
				ID:            "group-resource",
				AccountID:     accountID,
				Scope:         types.ResourceScopeServer,
				Name:          "组可见",
				URL:           "https://group.example.com",
				Enabled:       true,
				Visibility:    types.VisibilityRestricted,
				VisibleGroups: []string{"group-1"},
			},
			{
				ID:            "hidden-resource",
				AccountID:     accountID,
				Scope:         types.ResourceScopeServer,
				Name:          "不可见",
				URL:           "https://hidden.example.com",
				Enabled:       true,
				Visibility:    types.VisibilityRestricted,
				VisibleGroups: []string{"group-2"},
			},
			{
				ID:         "disabled-resource",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "已停用",
				URL:        "https://disabled.example.com",
				Enabled:    false,
				Visibility: types.VisibilityAll,
			},
		},
		userResources: map[string][]types.Resource{},
	}
	router := mux.NewRouter()
	AddEndpoints(workbenchManager.NewManager(memoryStore, nil), router)

	list := func() types.ResourceList {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/workbench/resources", nil)
		req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: accountID, UserId: userID})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		var response types.ResourceList
		if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		return response
	}

	initial := list()
	if len(initial.ServerResources) != 2 {
		t.Fatalf("initial server resources length = %d, want 2: %+v", len(initial.ServerResources), initial.ServerResources)
	}
	if initial.ServerResources[0].Visibility != "" || len(initial.ServerResources[1].VisibleGroups) != 0 {
		t.Fatalf("server resource visibility leaked to client: %+v", initial.ServerResources)
	}
	if len(initial.PersonalResources) != 0 {
		t.Fatalf("initial personal resources length = %d, want 0", len(initial.PersonalResources))
	}

	body := strings.NewReader(`{"name":"个人系统","url":"https://personal.example.com","category":"个人资源","tags":["常用"]}`)
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/resources", body)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: accountID, UserId: userID})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	afterCreate := list()
	if len(afterCreate.PersonalResources) != 1 {
		t.Fatalf("personal resources length = %d, want 1", len(afterCreate.PersonalResources))
	}
	personal := afterCreate.PersonalResources[0]
	if personal.Name != "个人系统" || personal.Scope != types.ResourceScopePersonal || personal.Source != types.ResourceSourceUser {
		t.Fatalf("personal resource mismatch: %+v", personal)
	}
	if personal.Visibility != "" || len(personal.VisibleGroups) != 0 || len(personal.VisibleUsers) != 0 {
		t.Fatalf("personal resource leaked visibility fields: %+v", personal)
	}
}

func TestPersonalIconUploadCanBeSavedOnPersonalResourceAndReadBack(t *testing.T) {
	accountID := "account-1"
	userID := "user-1"
	originalAssetsDir := assetsDir
	assetsDir = t.TempDir()
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	memoryStore := &httpWorkbenchStore{
		user: &nbtypes.User{
			Id:        userID,
			AccountID: accountID,
		},
		userResources: map[string][]types.Resource{},
	}
	router := mux.NewRouter()
	AddEndpoints(workbenchManager.NewManager(memoryStore, nil), router)

	uploadBody, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	uploadReq := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/icon", uploadBody)
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq = nbcontext.SetUserAuthInRequest(uploadReq, auth.UserAuth{AccountId: accountID, UserId: userID})
	uploadRecorder := httptest.NewRecorder()
	router.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d, body: %s", uploadRecorder.Code, http.StatusOK, uploadRecorder.Body.String())
	}
	var uploadResponse types.IconAssetResult
	if err := json.NewDecoder(uploadRecorder.Body).Decode(&uploadResponse); err != nil {
		t.Fatalf("Decode(upload) error = %v", err)
	}
	if !strings.HasPrefix(uploadResponse.IconURL, "/api/workbench/assets/") {
		t.Fatalf("upload icon URL = %q, want persistent asset URL", uploadResponse.IconURL)
	}
	if uploadResponse.IconMode != types.IconModeUploaded {
		t.Fatalf("upload icon mode = %q, want %q", uploadResponse.IconMode, types.IconModeUploaded)
	}

	payload, err := json.Marshal(types.Resource{
		Name:     "个人图标系统",
		URL:      "https://personal.example.com",
		IconURL:  uploadResponse.IconURL,
		IconMode: uploadResponse.IconMode,
	})
	if err != nil {
		t.Fatalf("Marshal(resource) error = %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/workbench/personal/resources", strings.NewReader(string(payload)))
	createReq = nbcontext.SetUserAuthInRequest(createReq, auth.UserAuth{AccountId: accountID, UserId: userID})
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body: %s", createRecorder.Code, http.StatusOK, createRecorder.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/workbench/resources", nil)
	listReq = nbcontext.SetUserAuthInRequest(listReq, auth.UserAuth{AccountId: accountID, UserId: userID})
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body: %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var listResponse types.ResourceList
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatalf("Decode(list) error = %v", err)
	}
	if len(listResponse.PersonalResources) != 1 {
		t.Fatalf("personal resources length = %d, want 1: %+v", len(listResponse.PersonalResources), listResponse.PersonalResources)
	}
	personal := listResponse.PersonalResources[0]
	if personal.IconURL != uploadResponse.IconURL || personal.IconMode != types.IconModeUploaded {
		t.Fatalf("personal icon fields = url %q mode %q, want %q/%q", personal.IconURL, personal.IconMode, uploadResponse.IconURL, types.IconModeUploaded)
	}

	assetReq := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(uploadResponse.IconURL, "/api"), nil)
	assetReq = nbcontext.SetUserAuthInRequest(assetReq, auth.UserAuth{AccountId: accountID, UserId: userID})
	assetRecorder := httptest.NewRecorder()
	router.ServeHTTP(assetRecorder, assetReq)
	if assetRecorder.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d, body: %s", assetRecorder.Code, http.StatusOK, assetRecorder.Body.String())
	}
	if !strings.HasPrefix(assetRecorder.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("asset content type = %q, want image/png", assetRecorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(assetRecorder.Body.String(), "icon") {
		t.Fatalf("asset body = %q, want uploaded icon content", assetRecorder.Body.String())
	}
}

func TestAdminIconUploadCanBeSavedOnServerResourceAndReadByClient(t *testing.T) {
	accountID := "account-1"
	adminID := "admin-1"
	originalAssetsDir := assetsDir
	assetsDir = t.TempDir()
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	memoryStore := &httpWorkbenchStore{
		user: &nbtypes.User{
			Id:        adminID,
			AccountID: accountID,
			Role:      nbtypes.UserRoleAdmin,
		},
		userResources: map[string][]types.Resource{},
	}
	router := mux.NewRouter()
	AddEndpoints(workbenchManager.NewManager(memoryStore, nil), router)

	uploadBody, contentType := multipartIconBody(t, "server.png", "\x89PNG\r\n\x1a\nserver-icon")
	uploadReq := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/icon", uploadBody)
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq = nbcontext.SetUserAuthInRequest(uploadReq, auth.UserAuth{AccountId: accountID, UserId: adminID})
	uploadRecorder := httptest.NewRecorder()
	router.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d, body: %s", uploadRecorder.Code, http.StatusOK, uploadRecorder.Body.String())
	}
	var uploadResponse types.IconAssetResult
	if err := json.NewDecoder(uploadRecorder.Body).Decode(&uploadResponse); err != nil {
		t.Fatalf("Decode(upload) error = %v", err)
	}
	if !strings.HasPrefix(uploadResponse.IconURL, "/api/workbench/assets/") {
		t.Fatalf("upload icon URL = %q, want persistent asset URL", uploadResponse.IconURL)
	}

	payload, err := json.Marshal(types.Resource{
		Name:       "服务器图标系统",
		URL:        "https://server.example.com",
		IconURL:    uploadResponse.IconURL,
		IconMode:   uploadResponse.IconMode,
		Enabled:    true,
		Visibility: types.VisibilityAll,
	})
	if err != nil {
		t.Fatalf("Marshal(resource) error = %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/workbench/admin/resources", strings.NewReader(string(payload)))
	createReq = nbcontext.SetUserAuthInRequest(createReq, auth.UserAuth{AccountId: accountID, UserId: adminID})
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status = %d, want %d, body: %s", createRecorder.Code, http.StatusOK, createRecorder.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/workbench/resources", nil)
	listReq = nbcontext.SetUserAuthInRequest(listReq, auth.UserAuth{AccountId: accountID, UserId: adminID})
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body: %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var listResponse types.ResourceList
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatalf("Decode(list) error = %v", err)
	}
	if len(listResponse.ServerResources) != 1 {
		t.Fatalf("server resources length = %d, want 1: %+v", len(listResponse.ServerResources), listResponse.ServerResources)
	}
	server := listResponse.ServerResources[0]
	if server.IconURL != uploadResponse.IconURL || server.IconMode != types.IconModeUploaded {
		t.Fatalf("server icon fields = url %q mode %q, want %q/%q", server.IconURL, server.IconMode, uploadResponse.IconURL, types.IconModeUploaded)
	}
	if server.Visibility != "" || len(server.VisibleGroups) != 0 || len(server.VisibleUsers) != 0 {
		t.Fatalf("server resource leaked visibility fields to client: %+v", server)
	}

	assetReq := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(uploadResponse.IconURL, "/api"), nil)
	assetReq = nbcontext.SetUserAuthInRequest(assetReq, auth.UserAuth{AccountId: accountID, UserId: adminID})
	assetRecorder := httptest.NewRecorder()
	router.ServeHTTP(assetRecorder, assetReq)
	if assetRecorder.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d, body: %s", assetRecorder.Code, http.StatusOK, assetRecorder.Body.String())
	}
	if !strings.HasPrefix(assetRecorder.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("asset content type = %q, want image/png", assetRecorder.Header().Get("Content-Type"))
	}
	if !strings.Contains(assetRecorder.Body.String(), "server-icon") {
		t.Fatalf("asset body = %q, want uploaded server icon content", assetRecorder.Body.String())
	}
}

func TestFetchIconUsesHTMLIconCandidates(t *testing.T) {
	pageURL := "https://example.com"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "", "/":
			return responseWithBody(http.StatusOK, "text/html", `<html><head><link rel="apple-touch-icon" href="/static/touch.png"></head></html>`), nil
		case "/favicon.ico", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png":
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		case "/static/touch.png":
			return responseWithBody(http.StatusOK, "image/png", "\x89PNG\r\n\x1a\nicon"), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		}
	})}

	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	parsed, err := url.Parse(pageURL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", pageURL, err)
	}
	content, contentType, sourceURL, err := fetchIconFromURL(req, client, parsed)
	if err != nil {
		t.Fatalf("fetchIconFromURL() error = %v", err)
	}
	if !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("contentType = %q, want image/png", contentType)
	}
	if sourceURL != pageURL+"/static/touch.png" {
		t.Fatalf("sourceURL = %q, want %q", sourceURL, pageURL+"/static/touch.png")
	}
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
}

func TestFetchIconAcceptsUppercaseHTTPScheme(t *testing.T) {
	pageURL := "HTTPS://example.com"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/favicon.ico":
			return responseWithBody(http.StatusOK, "image/png", "\x89PNG\r\n\x1a\nicon"), nil
		case "", "/", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png":
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		}
	})}

	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	parsed, err := url.Parse(pageURL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", pageURL, err)
	}
	content, contentType, sourceURL, err := fetchIconFromURL(req, client, parsed)
	if err != nil {
		t.Fatalf("fetchIconFromURL() error = %v", err)
	}
	if !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("contentType = %q, want image/png", contentType)
	}
	if sourceURL != "https://example.com/favicon.ico" {
		t.Fatalf("sourceURL = %q, want https://example.com/favicon.ico", sourceURL)
	}
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
}

func TestFetchIconUsesManifestIconCandidates(t *testing.T) {
	pageURL := "https://example.com"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "", "/":
			return responseWithBody(http.StatusOK, "text/html", `<html><head><link rel="manifest" href="/site.webmanifest"></head></html>`), nil
		case "/favicon.ico", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png":
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		case "/site.webmanifest":
			return responseWithBody(http.StatusOK, "application/manifest+json", `{"icons":[{"src":"/icons/app.png","sizes":"192x192"}]}`), nil
		case "/icons/app.png":
			return responseWithBody(http.StatusOK, "image/png", "\x89PNG\r\n\x1a\nicon"), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		}
	})}

	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	parsed, err := url.Parse(pageURL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", pageURL, err)
	}
	content, contentType, sourceURL, err := fetchIconFromURL(req, client, parsed)
	if err != nil {
		t.Fatalf("fetchIconFromURL() error = %v", err)
	}
	if !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("contentType = %q, want image/png", contentType)
	}
	if sourceURL != pageURL+"/icons/app.png" {
		t.Fatalf("sourceURL = %q, want %q", sourceURL, pageURL+"/icons/app.png")
	}
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
}

func TestFetchIconRejectsLocalAndPrivateHosts(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	for _, rawURL := range []string{
		"http://example.com/%zz",
		"http://example.com/\nicon.png",
		"http://localhost",
		"http://127.0.0.1",
		"http://[::1]",
		"http://10.0.0.1",
		"http://192.168.1.10",
		"http://169.254.169.254",
	} {
		t.Run(rawURL, func(t *testing.T) {
			if _, _, _, err := fetchIcon(req, rawURL); err == nil {
				t.Fatal("fetchIcon() error = nil, want local/private host rejection")
			}
		})
	}
}

func TestFetchIconRejectsHTMLCandidateLocalHosts(t *testing.T) {
	publicServerURL := "https://example.com"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == publicServerURL {
			return responseWithBody(http.StatusOK, "text/html", `<html><head><link rel="icon" href="http://127.0.0.1/internal.ico"></head></html>`), nil
		}
		if req.URL.Host == "127.0.0.1" {
			t.Fatalf("attempted to fetch local icon candidate %s", req.URL.String())
		}
		return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
	})}
	parsed, err := url.Parse(publicServerURL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", publicServerURL, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	if _, _, _, err := fetchIconFromURL(req, client, parsed); err == nil {
		t.Fatal("fetchIconFromURL() error = nil, want local candidate rejection")
	}
}

func TestFetchIconRejectsRedirectsToLocalHosts(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/workbench/personal/assets/fetch-icon", nil)
	client := newIconHTTPClient()
	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "example.com":
			return responseWithRedirect(req, "http://127.0.0.1/internal.ico"), nil
		case "127.0.0.1":
			t.Fatalf("attempted to follow local redirect %s", req.URL.String())
		}
		return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
	})

	if _, _, err := fetchIconURL(req, client, "https://example.com/favicon.ico"); err == nil {
		t.Fatal("fetchIconURL() error = nil, want redirect to local host rejection")
	}
}

func TestValidatePublicRemoteAddrRejectsPrivateAddresses(t *testing.T) {
	for _, remote := range []net.Addr{
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("100.64.0.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("169.254.169.254"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("198.51.100.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("203.0.113.1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("::1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("fc00::1"), Port: 443},
		&net.TCPAddr{IP: net.ParseIP("64:ff9b::808:808"), Port: 443},
	} {
		t.Run(remote.String(), func(t *testing.T) {
			if err := validatePublicRemoteAddr(remote); err == nil {
				t.Fatal("validatePublicRemoteAddr() error = nil, want private address rejection")
			}
		})
	}

	if err := validatePublicRemoteAddr(&net.TCPAddr{IP: net.ParseIP("93.184.216.34"), Port: 443}); err != nil {
		t.Fatalf("validatePublicRemoteAddr(public) error = %v", err)
	}
	if err := validatePublicRemoteAddr(&net.TCPAddr{IP: net.ParseIP("2606:4700:4700::1111"), Port: 443}); err != nil {
		t.Fatalf("validatePublicRemoteAddr(public IPv6) error = %v", err)
	}
}

func TestFetchIconRejectsNonImageCandidateContent(t *testing.T) {
	pageURL := "https://example.com"
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "", "/":
			return responseWithBody(http.StatusOK, "text/html", `<html><head><link rel="icon" href="/fake.png"></head></html>`), nil
		case "/favicon.ico", "/apple-touch-icon.png", "/apple-touch-icon-precomposed.png":
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		case "/fake.png":
			return responseWithBody(http.StatusOK, "image/png", "<html>not an image</html>"), nil
		default:
			return responseWithBody(http.StatusNotFound, "text/plain", "not found"), nil
		}
	})}
	parsed, err := url.Parse(pageURL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", pageURL, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/workbench/personal/assets/fetch-icon", nil)
	if _, _, _, err := fetchIconFromURL(req, client, parsed); err == nil {
		t.Fatal("fetchIconFromURL() error = nil, want non-image candidate rejection")
	}
}

func TestUploadAdminIconEndpointStoresAccountAsset(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	req := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "admin-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "admin-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 admin-1", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastAssetOwnerUserID != "" {
		t.Fatalf("admin asset owner user ID = %q, want empty", manager.lastAssetOwnerUserID)
	}
	var response types.IconAssetResult
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !strings.HasPrefix(response.IconURL, "/api/workbench/assets/") {
		t.Fatalf("IconURL = %q, want workbench asset URL", response.IconURL)
	}
	if response.IconMode != types.IconModeUploaded {
		t.Fatalf("IconMode = %q, want %q", response.IconMode, types.IconModeUploaded)
	}
}

func TestUploadPersonalIconEndpointStoresUserAsset(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "user-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 user-1", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastAssetOwnerUserID != "user-1" {
		t.Fatalf("personal asset owner user ID = %q, want user-1", manager.lastAssetOwnerUserID)
	}
	var response types.IconAssetResult
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !strings.HasPrefix(response.IconURL, "/api/workbench/assets/") {
		t.Fatalf("IconURL = %q, want workbench asset URL", response.IconURL)
	}
	if response.IconMode != types.IconModeUploaded {
		t.Fatalf("IconMode = %q, want %q", response.IconMode, types.IconModeUploaded)
	}
}

func TestUploadAdminIconCleansFileWhenAssetSaveFails(t *testing.T) {
	originalAssetsDir := assetsDir
	assetsDir = t.TempDir()
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	manager := &fakeWorkbenchManager{saveAdminAssetErr: errors.New("save failed")}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	req := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "admin-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want save failure", recorder.Code)
	}
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", assetsDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("asset files = %d, want cleanup on save failure", len(entries))
	}
}

func TestUploadPersonalIconCleansFileWhenAssetSaveFails(t *testing.T) {
	originalAssetsDir := assetsDir
	assetsDir = t.TempDir()
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	manager := &fakeWorkbenchManager{savePersonalAssetErr: errors.New("save failed")}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want save failure", recorder.Code)
	}
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", assetsDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("asset files = %d, want cleanup on save failure", len(entries))
	}
}

func TestUploadIconCleansMultipartTempFilesOnValidationFailure(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)

	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "large.png", "\x89PNG\r\n\x1a\n"+strings.Repeat("x", maxIconSize+1))
	req := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want oversized icon rejection", recorder.Code)
	}
	if manager.savePersonalAssetCalls != 0 {
		t.Fatalf("SavePersonalIconAsset calls = %d, want 0", manager.savePersonalAssetCalls)
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", tempDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("multipart temp files = %d, want cleanup", len(entries))
	}
}

func TestSaveIconFileRejectsPathTraversalAssetID(t *testing.T) {
	parentDir := t.TempDir()
	originalAssetsDir := assetsDir
	assetsDir = filepath.Join(parentDir, "assets")
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	for _, assetID := range []string{"", ".", "..", "../evil", "nested/evil", `nested\evil`} {
		t.Run(assetID, func(t *testing.T) {
			path, _, err := saveIconFile(assetID, []byte("\x89PNG\r\n\x1a\nicon"))
			if err == nil {
				t.Fatalf("saveIconFile(%q) error = nil, want rejection", assetID)
			}
			if path != "" {
				t.Fatalf("saveIconFile(%q) path = %q, want empty", assetID, path)
			}
		})
	}

	if _, err := os.Stat(filepath.Join(parentDir, "evil")); !os.IsNotExist(err) {
		t.Fatalf("path traversal file exists or stat failed unexpectedly: %v", err)
	}
}

func TestSetAssetsDirFromDataDirUsesWorkbenchAssetsSubdir(t *testing.T) {
	originalAssetsDir := assetsDir
	t.Cleanup(func() {
		assetsDir = originalAssetsDir
	})

	dataDir := t.TempDir()
	SetAssetsDirFromDataDir("  " + dataDir + "  ")
	if got, want := assetsDir, filepath.Join(dataDir, "workbench", "assets"); got != want {
		t.Fatalf("assetsDir = %q, want %q", got, want)
	}

	SetAssetsDirFromDataDir("  ")
	if got, want := assetsDir, filepath.Join(dataDir, "workbench", "assets"); got != want {
		t.Fatalf("assetsDir changed on empty datadir: %q, want %q", got, want)
	}
}

func TestUploadAdminIconEndpointRejectsOversizedMultipartBody(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	body, contentType := multipartIconBody(t, "huge.png", string(make([]byte, maxIconRequestSize+1)))
	req := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/icon", body)
	req.Header.Set("Content-Type", contentType)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "admin-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want upload rejection", recorder.Code)
	}
	if manager.saveAdminAssetCalls != 0 {
		t.Fatalf("SaveAdminIconAsset calls = %d, want 0", manager.saveAdminAssetCalls)
	}
}

func TestAdminIconEndpointsCheckPermissionBeforeWork(t *testing.T) {
	manager := &fakeWorkbenchManager{adminAssetAccessErr: status.NewPermissionDeniedError()}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	uploadBody, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	uploadReq := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/icon", uploadBody)
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq = nbcontext.SetUserAuthInRequest(uploadReq, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	uploadRecorder := httptest.NewRecorder()
	router.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code == http.StatusOK {
		t.Fatal("upload status OK, want permission rejection")
	}
	if manager.saveAdminAssetCalls != 0 {
		t.Fatalf("SaveAdminIconAsset calls = %d, want 0", manager.saveAdminAssetCalls)
	}

	fetchReq := httptest.NewRequest(http.MethodPost, "/workbench/admin/assets/fetch-icon", strings.NewReader(`{"url":"https://example.com"}`))
	fetchReq = nbcontext.SetUserAuthInRequest(fetchReq, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	fetchRecorder := httptest.NewRecorder()
	router.ServeHTTP(fetchRecorder, fetchReq)
	if fetchRecorder.Code == http.StatusOK {
		t.Fatal("fetch status OK, want permission rejection")
	}
	if manager.saveAdminAssetCalls != 0 {
		t.Fatalf("SaveAdminIconAsset calls after fetch = %d, want 0", manager.saveAdminAssetCalls)
	}
}

func TestPersonalIconEndpointsCheckUserBeforeWork(t *testing.T) {
	manager := &fakeWorkbenchManager{personalAssetAccessErr: status.NewPermissionDeniedError()}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	uploadBody, contentType := multipartIconBody(t, "icon.png", "\x89PNG\r\n\x1a\nicon")
	uploadReq := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/icon", uploadBody)
	uploadReq.Header.Set("Content-Type", contentType)
	uploadReq = nbcontext.SetUserAuthInRequest(uploadReq, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	uploadRecorder := httptest.NewRecorder()
	router.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code == http.StatusOK {
		t.Fatal("upload status OK, want user access rejection")
	}
	if manager.savePersonalAssetCalls != 0 {
		t.Fatalf("SavePersonalIconAsset calls = %d, want 0", manager.savePersonalAssetCalls)
	}

	fetchReq := httptest.NewRequest(http.MethodPost, "/workbench/personal/assets/fetch-icon", strings.NewReader(`{"url":"https://example.com"}`))
	fetchReq = nbcontext.SetUserAuthInRequest(fetchReq, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	fetchRecorder := httptest.NewRecorder()
	router.ServeHTTP(fetchRecorder, fetchReq)
	if fetchRecorder.Code == http.StatusOK {
		t.Fatal("fetch status OK, want user access rejection")
	}
	if manager.savePersonalAssetCalls != 0 {
		t.Fatalf("SavePersonalIconAsset calls after fetch = %d, want 0", manager.savePersonalAssetCalls)
	}
}

func TestFetchIconEndpointsRejectOversizedJSONBody(t *testing.T) {
	manager := &fakeWorkbenchManager{}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	oversizedBody := `{"url":"https://example.com/` + strings.Repeat("x", maxJSONRequestSize+1) + `"}`
	tests := []struct {
		name string
		path string
		auth auth.UserAuth
	}{
		{
			name: "personal",
			path: "/workbench/personal/assets/fetch-icon",
			auth: auth.UserAuth{AccountId: "account-1", UserId: "user-1"},
		},
		{
			name: "admin",
			path: "/workbench/admin/assets/fetch-icon",
			auth: auth.UserAuth{AccountId: "account-1", UserId: "admin-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(oversizedBody))
			req = nbcontext.SetUserAuthInRequest(req, tt.auth)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code == http.StatusOK {
				t.Fatalf("status = %d, want oversized JSON rejection", recorder.Code)
			}
		})
	}
	if manager.savePersonalAssetCalls != 0 {
		t.Fatalf("SavePersonalIconAsset calls = %d, want 0", manager.savePersonalAssetCalls)
	}
	if manager.saveAdminAssetCalls != 0 {
		t.Fatalf("SaveAdminIconAsset calls = %d, want 0", manager.saveAdminAssetCalls)
	}
}

func TestGetAssetEndpointUsesAuthContextAndServesFile(t *testing.T) {
	content := []byte("\x89PNG\r\n\x1a\nicon")
	path := t.TempDir() + "/icon.png"
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager := &fakeWorkbenchManager{
		asset: &types.Asset{
			ID:          "asset-1",
			AccountID:   "account-1",
			OwnerUserID: "user-1",
			StoragePath: path,
			ContentType: "image/png",
			CreatedAt:   time.Now(),
		},
	}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/assets/asset-1", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "user-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 user-1", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastAssetID != "asset-1" {
		t.Fatalf("asset ID = %q, want asset-1", manager.lastAssetID)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/png") {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got, want := recorder.Header().Get("X-Content-Type-Options"), "nosniff"; got != want {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, want)
	}
	if got, want := recorder.Header().Get("Referrer-Policy"), "no-referrer"; got != want {
		t.Fatalf("Referrer-Policy = %q, want %q", got, want)
	}
	csp := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") ||
		!strings.Contains(csp, "script-src 'none'") ||
		!strings.Contains(csp, "sandbox") {
		t.Fatalf("Content-Security-Policy = %q, want restrictive asset CSP", csp)
	}
	if got := recorder.Body.Bytes(); string(got) != string(content) {
		t.Fatalf("body = %q, want %q", string(got), string(content))
	}
}

func TestGetAssetEndpointReturnsNotFoundWhenAssetFileMissing(t *testing.T) {
	manager := &fakeWorkbenchManager{
		asset: &types.Asset{
			ID:          "asset-1",
			AccountID:   "account-1",
			OwnerUserID: "user-1",
			StoragePath: filepath.Join(t.TempDir(), "missing.png"),
			ContentType: "image/png",
			CreatedAt:   time.Now(),
		},
	}
	router := mux.NewRouter()
	AddEndpoints(manager, router)

	req := httptest.NewRequest(http.MethodGet, "/workbench/assets/asset-1", nil)
	req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{AccountId: "account-1", UserId: "user-1"})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = %d, want missing asset file rejection", recorder.Code)
	}
	if manager.lastAccountID != "account-1" || manager.lastUserID != "user-1" {
		t.Fatalf("manager auth = account %q user %q, want account-1 user-1", manager.lastAccountID, manager.lastUserID)
	}
	if manager.lastAssetID != "asset-1" {
		t.Fatalf("asset ID = %q, want asset-1", manager.lastAssetID)
	}
}

func TestReadIconAcceptsSVGAndRejectsPlainText(t *testing.T) {
	content, contentType, err := readIcon(strings.NewReader(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	if err != nil {
		t.Fatalf("readIcon(svg) error = %v", err)
	}
	if len(content) == 0 {
		t.Fatal("readIcon(svg) content is empty")
	}
	if got, want := contentType, "image/svg+xml"; got != want {
		t.Fatalf("readIcon(svg) contentType = %q, want %q", got, want)
	}

	if _, _, err := readIcon(strings.NewReader("plain text")); err == nil {
		t.Fatal("readIcon(text) error = nil, want unsupported icon type")
	}
}

type fakeWorkbenchManager struct {
	lastAccountID          string
	lastUserID             string
	lastResourceID         string
	lastScope              string
	lastAssetID            string
	lastResource           *types.Resource
	asset                  *types.Asset
	lastAssetOwnerUserID   string
	listResources          *types.ResourceList
	adminAssetAccessErr    error
	personalAssetAccessErr error
	saveAdminAssetErr      error
	savePersonalAssetErr   error
	saveAdminAssetCalls    int
	savePersonalAssetCalls int
}

func (m *fakeWorkbenchManager) remember(accountID, userID string) {
	m.lastAccountID = accountID
	m.lastUserID = userID
}

func (m *fakeWorkbenchManager) ListResources(ctx context.Context, accountID, userID string) (*types.ResourceList, error) {
	m.remember(accountID, userID)
	return m.listResources, nil
}

func (m *fakeWorkbenchManager) ListAdminResources(ctx context.Context, accountID, userID string) ([]*types.Resource, error) {
	m.remember(accountID, userID)
	return []*types.Resource{{ID: "server-list-resource", Scope: types.ResourceScopeServer, Name: "Server List Resource", URL: "https://server-list.example.com"}}, nil
}

func (m *fakeWorkbenchManager) GetAdminResource(ctx context.Context, accountID, userID, resourceID string) (*types.Resource, error) {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	return &types.Resource{ID: resourceID, Scope: types.ResourceScopeServer, Name: "Server Resource", URL: "https://server.example.com"}, nil
}

func (m *fakeWorkbenchManager) CreateAdminResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error) {
	m.remember(accountID, userID)
	m.lastResource = resource
	resource.Scope = types.ResourceScopeServer
	return resource, nil
}

func (m *fakeWorkbenchManager) UpdateAdminResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error) {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	m.lastResource = resource
	return resource, nil
}

func (m *fakeWorkbenchManager) DeleteAdminResource(ctx context.Context, accountID, userID, resourceID string) error {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	return nil
}

func (m *fakeWorkbenchManager) CreatePersonalResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error) {
	m.remember(accountID, userID)
	m.lastResource = resource
	resource.Scope = types.ResourceScopePersonal
	return resource, nil
}

func (m *fakeWorkbenchManager) UpdatePersonalResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error) {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	m.lastResource = resource
	resource.ID = resourceID
	resource.Scope = types.ResourceScopePersonal
	return resource, nil
}

func (m *fakeWorkbenchManager) DeletePersonalResource(ctx context.Context, accountID, userID, resourceID string) error {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	return nil
}

func (m *fakeWorkbenchManager) RecordLaunch(ctx context.Context, accountID, userID, resourceID, scope string) error {
	m.remember(accountID, userID)
	m.lastResourceID = resourceID
	m.lastScope = scope
	return nil
}

func (m *fakeWorkbenchManager) EnsureAdminAssetAccess(ctx context.Context, accountID, userID string) error {
	m.remember(accountID, userID)
	return m.adminAssetAccessErr
}

func (m *fakeWorkbenchManager) EnsurePersonalAssetAccess(ctx context.Context, accountID, userID string) error {
	m.remember(accountID, userID)
	return m.personalAssetAccessErr
}

func (m *fakeWorkbenchManager) SavePersonalIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error) {
	m.remember(accountID, userID)
	m.savePersonalAssetCalls++
	m.lastAssetOwnerUserID = userID
	if m.savePersonalAssetErr != nil {
		return nil, m.savePersonalAssetErr
	}
	return &types.Asset{ID: assetID, PublicURL: publicURL, OwnerUserID: userID}, nil
}

func (m *fakeWorkbenchManager) SaveAdminIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error) {
	m.remember(accountID, userID)
	m.saveAdminAssetCalls++
	m.lastAssetOwnerUserID = ""
	if m.saveAdminAssetErr != nil {
		return nil, m.saveAdminAssetErr
	}
	return &types.Asset{ID: assetID, PublicURL: publicURL}, nil
}

func (m *fakeWorkbenchManager) GetAsset(ctx context.Context, accountID, userID, assetID string) (*types.Asset, error) {
	m.remember(accountID, userID)
	m.lastAssetID = assetID
	if m.asset != nil {
		return m.asset, nil
	}
	return &types.Asset{ID: assetID}, nil
}

type httpWorkbenchStore struct {
	user            *nbtypes.User
	groups          map[string]*nbtypes.Group
	serverResources []*types.Resource
	userResources   map[string][]types.Resource
	recentVisits    map[string][]types.RecentVisit
	asset           *types.Asset
}

func (s *httpWorkbenchStore) GetUserByUserID(ctx context.Context, lockStrength store.LockingStrength, userID string) (*nbtypes.User, error) {
	if s.user != nil && s.user.Id == userID {
		return s.user, nil
	}
	return nil, status.Errorf(status.NotFound, "user not found")
}

func (s *httpWorkbenchStore) GetGroupsByIDs(ctx context.Context, lockStrength store.LockingStrength, accountID string, groupIDs []string) (map[string]*nbtypes.Group, error) {
	result := make(map[string]*nbtypes.Group)
	for _, groupID := range groupIDs {
		group := s.groups[groupID]
		if group != nil && group.AccountID == accountID {
			result[groupID] = group
		}
	}
	return result, nil
}

func (s *httpWorkbenchStore) GetWorkbenchServerResources(ctx context.Context, accountID string) ([]*types.Resource, error) {
	resources := make([]*types.Resource, 0, len(s.serverResources))
	for _, resource := range s.serverResources {
		if resource.AccountID == accountID {
			copyResource := *resource
			resources = append(resources, &copyResource)
		}
	}
	return resources, nil
}

func (s *httpWorkbenchStore) GetWorkbenchServerResource(ctx context.Context, accountID, resourceID string) (*types.Resource, error) {
	for _, resource := range s.serverResources {
		if resource.AccountID == accountID && resource.ID == resourceID {
			copyResource := *resource
			return &copyResource, nil
		}
	}
	return nil, status.Errorf(status.NotFound, "workbench resource not found")
}

func (s *httpWorkbenchStore) SaveWorkbenchServerResource(ctx context.Context, resource *types.Resource) error {
	for i := range s.serverResources {
		if s.serverResources[i].AccountID == resource.AccountID && s.serverResources[i].ID == resource.ID {
			copyResource := *resource
			s.serverResources[i] = &copyResource
			return nil
		}
	}
	copyResource := *resource
	s.serverResources = append(s.serverResources, &copyResource)
	return nil
}

func (s *httpWorkbenchStore) DeleteWorkbenchServerResource(ctx context.Context, accountID, resourceID string) error {
	for i := range s.serverResources {
		if s.serverResources[i].AccountID == accountID && s.serverResources[i].ID == resourceID {
			s.serverResources = append(s.serverResources[:i], s.serverResources[i+1:]...)
			return nil
		}
	}
	return status.Errorf(status.NotFound, "workbench resource not found")
}

func (s *httpWorkbenchStore) GetWorkbenchUserResources(ctx context.Context, accountID, userID string) ([]types.Resource, error) {
	resources := s.userResources[accountID+"/"+userID]
	result := make([]types.Resource, len(resources))
	copy(result, resources)
	return result, nil
}

func (s *httpWorkbenchStore) GetWorkbenchUserState(ctx context.Context, accountID, userID string) (*types.UserResources, error) {
	key := accountID + "/" + userID
	resources := s.userResources[key]
	recentVisits := s.recentVisits[key]
	resultResources := make([]types.Resource, len(resources))
	copy(resultResources, resources)
	resultRecentVisits := make([]types.RecentVisit, len(recentVisits))
	copy(resultRecentVisits, recentVisits)
	return &types.UserResources{
		AccountID:     accountID,
		UserID:        userID,
		ResourcesJSON: resultResources,
		RecentVisits:  resultRecentVisits,
	}, nil
}

func (s *httpWorkbenchStore) SaveWorkbenchUserResources(ctx context.Context, accountID, userID string, resources []types.Resource) error {
	if s.userResources == nil {
		s.userResources = map[string][]types.Resource{}
	}
	copyResources := make([]types.Resource, len(resources))
	copy(copyResources, resources)
	s.userResources[accountID+"/"+userID] = copyResources
	return nil
}

func (s *httpWorkbenchStore) SaveWorkbenchUserRecentVisits(ctx context.Context, accountID, userID string, recentVisits []types.RecentVisit) error {
	if s.recentVisits == nil {
		s.recentVisits = map[string][]types.RecentVisit{}
	}
	copyRecentVisits := make([]types.RecentVisit, len(recentVisits))
	copy(copyRecentVisits, recentVisits)
	s.recentVisits[accountID+"/"+userID] = copyRecentVisits
	return nil
}

func (s *httpWorkbenchStore) SaveWorkbenchAsset(ctx context.Context, asset *types.Asset) error {
	copyAsset := *asset
	s.asset = &copyAsset
	return nil
}

func (s *httpWorkbenchStore) GetWorkbenchAsset(ctx context.Context, accountID, assetID string) (*types.Asset, error) {
	if s.asset != nil && s.asset.AccountID == accountID && s.asset.ID == assetID {
		copyAsset := *s.asset
		return &copyAsset, nil
	}
	return nil, status.Errorf(status.NotFound, "workbench asset not found")
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func responseWithBody(statusCode int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    httptest.NewRequest(http.MethodGet, "https://example.com", nil),
	}
}

func responseWithRedirect(req *http.Request, location string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{location}},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}
}

func multipartIconBody(t *testing.T, filename, content string) (*strings.Reader, string) {
	t.Helper()
	var body strings.Builder
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return strings.NewReader(body.String()), writer.FormDataContentType()
}

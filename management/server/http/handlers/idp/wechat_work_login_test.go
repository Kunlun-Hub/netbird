package idp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/idp/dex"
)

func TestBuildWeChatWorkLoginURL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://internal/oauth2/callback/wechatwork-connector?state=auth-req", nil)
	req.Host = "sso.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "login.example.com")

	loginURL, err := buildWeChatWorkLoginURL(req, "wwsuiteid", "auth-req")
	require.NoError(t, err)

	parsed, err := url.Parse(loginURL)
	require.NoError(t, err)
	assert.Equal(t, weChatWorkLoginURL, parsed.Scheme+"://"+parsed.Host+parsed.Path)
	assert.Equal(t, "ServiceApp", parsed.Query().Get("login_type"))
	assert.Equal(t, "wwsuiteid", parsed.Query().Get("appid"))
	assert.Equal(t, "auth-req", parsed.Query().Get("state"))
	assert.Equal(t, "https://login.example.com/oauth2/callback/wechatwork-connector", parsed.Query().Get("redirect_uri"))
}

func TestResolveWeChatWorkIdentity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/service/get_suite_token", func(w http.ResponseWriter, r *http.Request) {
		var req weChatWorkSuiteTokenRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "wwsuiteid", req.SuiteID)
		assert.Equal(t, "suite-secret", req.SuiteSecret)
		assert.Equal(t, "suite-ticket", req.SuiteTicket)
		require.NoError(t, json.NewEncoder(w).Encode(weChatWorkSuiteTokenResponse{
			ErrCode:          0,
			ErrMsg:           "ok",
			SuiteAccessToken: "suite-access-token",
		}))
	})
	mux.HandleFunc("/cgi-bin/service/auth/getuserinfo3rd", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "suite-access-token", r.URL.Query().Get("suite_access_token"))
		assert.Equal(t, "auth-code", r.URL.Query().Get("code"))
		require.NoError(t, json.NewEncoder(w).Encode(weChatWorkUserInfoResponse{
			ErrCode:    0,
			ErrMsg:     "ok",
			UserID:     "zhangsan",
			OpenUserID: "open-user-id",
			UserTicket: "user-ticket",
			CorpID:     "wwcorp",
		}))
	})
	mux.HandleFunc("/cgi-bin/service/auth/getuserdetail3rd", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "suite-access-token", r.URL.Query().Get("suite_access_token"))
		var req weChatWorkUserDetailRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "user-ticket", req.UserTicket)
		require.NoError(t, json.NewEncoder(w).Encode(weChatWorkUserDetailResponse{
			ErrCode: 0,
			ErrMsg:  "ok",
			Name:    "张三",
			Email:   "zhangsan@example.com",
		}))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	oldSuiteTokenURL := weChatWorkSuiteTokenURL
	oldUserInfoURL := weChatWorkUserInfoURL
	oldUserDetailURL := weChatWorkUserDetailURL
	weChatWorkSuiteTokenURL = server.URL + "/cgi-bin/service/get_suite_token"
	weChatWorkUserInfoURL = server.URL + "/cgi-bin/service/auth/getuserinfo3rd"
	weChatWorkUserDetailURL = server.URL + "/cgi-bin/service/auth/getuserdetail3rd"
	t.Cleanup(func() {
		weChatWorkSuiteTokenURL = oldSuiteTokenURL
		weChatWorkUserInfoURL = oldUserInfoURL
		weChatWorkUserDetailURL = oldUserDetailURL
	})

	handler := &weChatWorkCallbackHandler{
		httpClient: server.Client(),
	}

	identity, err := handler.resolveWeChatWorkIdentity(context.Background(), &dex.ConnectorConfig{
		ClientID:     "wwsuiteid",
		ClientSecret: "suite-secret",
		SuiteTicket:  "suite-ticket",
	}, "auth-code")
	require.NoError(t, err)
	assert.Equal(t, "wwcorp:zhangsan", identity.UserID)
	assert.Equal(t, "wwcorp:zhangsan", identity.Username)
	assert.Equal(t, "张三", identity.Name)
	assert.Equal(t, "zhangsan@example.com", identity.Email)
}

package idp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/netbirdio/netbird/idp/dex"
	idpmanager "github.com/netbirdio/netbird/management/server/idp"
)

const weChatWorkLoginURL = "https://login.work.weixin.qq.com/wwlogin/sso/login"

var (
	weChatWorkSuiteTokenURL = "https://qyapi.weixin.qq.com/cgi-bin/service/get_suite_token"
	weChatWorkUserInfoURL   = "https://qyapi.weixin.qq.com/cgi-bin/service/auth/getuserinfo3rd"
	weChatWorkUserDetailURL = "https://qyapi.weixin.qq.com/cgi-bin/service/auth/getuserdetail3rd"
)

type weChatWorkCallbackHandler struct {
	embeddedIDP *idpmanager.EmbeddedIdPManager
	dexHandler  http.Handler
	httpClient  *http.Client
}

type weChatWorkSuiteTokenRequest struct {
	SuiteID     string `json:"suite_id"`
	SuiteSecret string `json:"suite_secret"`
	SuiteTicket string `json:"suite_ticket"`
}

type weChatWorkSuiteTokenResponse struct {
	ErrCode          int    `json:"errcode"`
	ErrMsg           string `json:"errmsg"`
	SuiteAccessToken string `json:"suite_access_token"`
}

type weChatWorkUserInfoResponse struct {
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
	UserID     string `json:"userid"`
	OpenUserID string `json:"open_userid"`
	UserTicket string `json:"user_ticket"`
	CorpID     string `json:"corpid"`
}

type weChatWorkUserDetailRequest struct {
	UserTicket string `json:"user_ticket"`
}

type weChatWorkUserDetailResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Name    string `json:"name"`
	Email   string `json:"email"`
}

func NewWeChatWorkCallbackHandler(embeddedIDP *idpmanager.EmbeddedIdPManager) http.Handler {
	return &weChatWorkCallbackHandler{
		embeddedIDP: embeddedIDP,
		dexHandler:  embeddedIDP.Handler(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (h *weChatWorkCallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	connectorID := mux.Vars(r)["connector"]
	if connectorID == "" {
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	connector, err := h.embeddedIDP.GetConnector(r.Context(), connectorID)
	if err != nil || connector.Type != "wechatwork" {
		h.dexHandler.ServeHTTP(w, r)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		http.Error(w, "missing state parameter", http.StatusBadRequest)
		return
	}

	if errType := r.URL.Query().Get("error"); errType != "" {
		http.Error(w, r.URL.Query().Get("error_description"), http.StatusBadRequest)
		return
	}

	if r.URL.Query().Get("code") == "" {
		loginURL, err := buildWeChatWorkLoginURL(r, connector.ClientID, state)
		if err != nil {
			http.Error(w, "failed to build WeChat Work login URL", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, loginURL, http.StatusFound)
		return
	}

	identity, err := h.resolveWeChatWorkIdentity(r.Context(), connector, r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	proxiedReq := r.Clone(r.Context())
	proxiedReq.URL = cloneURL(r.URL)
	proxiedReq.URL.RawQuery = url.Values{"state": []string{state}}.Encode()
	proxiedReq.Header = r.Header.Clone()
	proxiedReq.Header.Set("X-NetBird-WeChatWork-User-Id", identity.UserID)
	proxiedReq.Header.Set("X-NetBird-WeChatWork-User", identity.Username)
	proxiedReq.Header.Set("X-NetBird-WeChatWork-User-Name", identity.Name)
	proxiedReq.Header.Set("X-NetBird-WeChatWork-User-Email", identity.Email)

	h.dexHandler.ServeHTTP(w, proxiedReq)
}

type weChatWorkIdentity struct {
	UserID   string
	Username string
	Name     string
	Email    string
}

func (h *weChatWorkCallbackHandler) resolveWeChatWorkIdentity(ctx context.Context, connector *dex.ConnectorConfig, code string) (*weChatWorkIdentity, error) {
	if connector.ClientID == "" || connector.ClientSecret == "" || connector.SuiteTicket == "" {
		return nil, fmt.Errorf("wechat work connector is missing suite configuration")
	}

	var suiteToken weChatWorkSuiteTokenResponse
	if err := postJSON(ctx, h.httpClient, weChatWorkSuiteTokenURL, weChatWorkSuiteTokenRequest{
		SuiteID:     connector.ClientID,
		SuiteSecret: connector.ClientSecret,
		SuiteTicket: connector.SuiteTicket,
	}, &suiteToken); err != nil {
		return nil, err
	}
	if suiteToken.ErrCode != 0 || suiteToken.SuiteAccessToken == "" {
		return nil, fmt.Errorf("failed to get suite_access_token: %s (%d)", suiteToken.ErrMsg, suiteToken.ErrCode)
	}

	var userInfo weChatWorkUserInfoResponse
	if err := getJSON(ctx, h.httpClient, weChatWorkUserInfoURL, url.Values{
		"suite_access_token": []string{suiteToken.SuiteAccessToken},
		"code":               []string{code},
	}, &userInfo); err != nil {
		return nil, err
	}
	if userInfo.ErrCode != 0 {
		return nil, fmt.Errorf("failed to get WeChat Work user info: %s (%d)", userInfo.ErrMsg, userInfo.ErrCode)
	}

	userID := userInfo.OpenUserID
	if userID == "" {
		userID = userInfo.UserID
	}
	if userID == "" {
		return nil, fmt.Errorf("wechat work user identity is missing userid")
	}
	if userInfo.CorpID != "" && userInfo.UserID != "" {
		userID = userInfo.CorpID + ":" + userInfo.UserID
	}

	identity := &weChatWorkIdentity{
		UserID:   userID,
		Username: userID,
		Name:     userID,
		Email:    sanitizeWeChatWorkEmail(userID),
	}

	if userInfo.UserTicket != "" {
		var detail weChatWorkUserDetailResponse
		if err := postJSON(ctx, h.httpClient, weChatWorkUserDetailURL+"?suite_access_token="+url.QueryEscape(suiteToken.SuiteAccessToken), weChatWorkUserDetailRequest{
			UserTicket: userInfo.UserTicket,
		}, &detail); err == nil && detail.ErrCode == 0 {
			if detail.Name != "" {
				identity.Name = detail.Name
			}
			if detail.Email != "" {
				identity.Email = detail.Email
			}
		}
	}

	return identity, nil
}

func buildWeChatWorkLoginURL(r *http.Request, suiteID, state string) (string, error) {
	redirectURI := currentRequestURL(r)
	if redirectURI == "" {
		return "", fmt.Errorf("missing callback URL")
	}

	loginURL, err := url.Parse(weChatWorkLoginURL)
	if err != nil {
		return "", err
	}

	loginURL.RawQuery = url.Values{
		"login_type":   []string{"ServiceApp"},
		"appid":        []string{suiteID},
		"redirect_uri": []string{redirectURI},
		"state":        []string{state},
	}.Encode()

	return loginURL.String(), nil
}

func currentRequestURL(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := r.Host
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	if host == "" {
		return ""
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   r.URL.Path,
	}

	return u.String()
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return &url.URL{}
	}
	clone := *u
	return &clone
}

func postJSON[T any](ctx context.Context, client *http.Client, endpoint string, payload any, target *T) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func getJSON[T any](ctx context.Context, client *http.Client, endpoint string, query url.Values, target *T) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func sanitizeWeChatWorkEmail(id string) string {
	replacer := strings.NewReplacer(":", ".", "/", ".", "\\", ".", " ", ".")
	sanitized := replacer.Replace(id)
	if sanitized == "" {
		sanitized = "user"
	}
	return sanitized + "@wechatwork.local"
}

package licensing

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/netbirdio/netbird/management/server/entitlements"
)

const (
	licenseFileName = "license.json"

	licenseSecretEnv = "CLOINK_LICENSE_AES_KEY"
	defaultSecret    = "lA8fsCkh1s7e2JEruZCr0JNChQIfpuDbr6avPSbWgasz2cseGjNcZ225BAuCy4m2CDz8jMSHaQxHSWBXxfo1viFDDZRTDJqQ82oFfietnjhEYpuG1DPslfIpyFLiSvse"
)

type Status string

const (
	StatusUnlicensed  Status = "unlicensed"
	StatusActive      Status = "active"
	StatusInvalid     Status = "invalid"
	StatusURLMismatch Status = "url_mismatch"
	StatusNotStarted  Status = "not_started"
	StatusExpired     Status = "expired"
)

type LicenseType string

const (
	LicenseTypeTrial      LicenseType = "try"
	LicenseTypeYear       LicenseType = "year"
	LicenseTypeEnterprise LicenseType = "enterprise"
)

var ErrInvalidLicenseKey = errors.New("invalid license key")

type State struct {
	MachineID        string
	ServerURL        string
	Status           Status
	Plan             entitlements.Plan
	LicenseKeyMasked string
	LicenseTypes     []LicenseType
	Message          string
	StartTime        *time.Time
	EndTime          *time.Time
	UpdatedAt        *time.Time
}

type licensePayload struct {
	ServerURL    string
	LicenseTypes []LicenseType
	Key          string
	StartTime    *time.Time
	EndTime      *time.Time
}

type storedLicense struct {
	Key       string    `json:"key"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Manager struct {
	datadir string
	secret  string
	now     func() time.Time
	mu      sync.Mutex
}

type Option func(*Manager)

func WithSecret(secret string) Option {
	return func(m *Manager) {
		m.secret = secret
	}
}

func WithNow(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

func NewManager(datadir string, options ...Option) *Manager {
	manager := &Manager{
		datadir: datadir,
		secret:  licenseSecret(),
		now:     time.Now,
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func NewEntitlementsProvider(manager *Manager) *EntitlementsProvider {
	return &EntitlementsProvider{manager: manager}
}

type EntitlementsProvider struct {
	manager *Manager
}

func (p *EntitlementsProvider) GetEntitlements(ctx context.Context, accountID string) (entitlements.Entitlements, error) {
	if p == nil || p.manager == nil {
		snapshot, err := entitlements.PlanEntitlements(entitlements.PlanBasic)
		snapshot.AccountID = accountID
		return snapshot, err
	}

	state, err := p.manager.GetState(ctx, "")
	if err != nil {
		return entitlements.Entitlements{}, err
	}

	snapshot, err := entitlements.PlanEntitlements(state.Plan)
	if err != nil {
		return entitlements.Entitlements{}, err
	}
	snapshot.AccountID = accountID
	return snapshot, nil
}

func (m *Manager) MachineID(_ context.Context, serverURL string) (string, error) {
	machineID := NormalizeServerURL(serverURL)
	if machineID == "" {
		return "", errors.New("dashboard server URL is empty")
	}
	return machineID, nil
}

func (m *Manager) GetState(ctx context.Context, serverURL string) (*State, error) {
	machineID := NormalizeServerURL(serverURL)

	stored, found, err := m.readStoredLicense()
	if err != nil {
		return nil, err
	}
	if !found || strings.TrimSpace(stored.Key) == "" {
		return m.state(machineID, "", "", StatusUnlicensed, entitlements.PlanBasic, nil, nil, nil, "No license key has been installed."), nil
	}

	state := m.validate(ctx, machineID, stored.Key)
	state.UpdatedAt = &stored.UpdatedAt
	return state, nil
}

func (m *Manager) UpdateKey(ctx context.Context, serverURL, key string) (*State, error) {
	machineID, err := m.MachineID(ctx, serverURL)
	if err != nil {
		return nil, err
	}

	key = strings.TrimSpace(key)
	if key == "" {
		if err := m.deleteStoredLicense(); err != nil {
			return nil, err
		}
		return m.state(machineID, "", "", StatusUnlicensed, entitlements.PlanBasic, nil, nil, nil, "No license key has been installed."), nil
	}

	state := m.validate(ctx, machineID, key)
	if state.Status != StatusActive {
		return state, ErrInvalidLicenseKey
	}

	stored := storedLicense{
		Key:       key,
		UpdatedAt: m.now().UTC(),
	}
	if err := m.writeStoredLicense(stored); err != nil {
		return nil, err
	}
	state.UpdatedAt = &stored.UpdatedAt
	return state, nil
}

func (m *Manager) validate(_ context.Context, machineID, key string) *State {
	payload, err := DecryptLicensePayload(key, m.secret)
	if err != nil {
		return m.state(machineID, "", key, StatusInvalid, entitlements.PlanBasic, nil, nil, nil, "License key cannot be decrypted.")
	}

	payload.ServerURL = NormalizeServerURL(payload.ServerURL)
	if payload.ServerURL == "" {
		return m.state(machineID, payload.ServerURL, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License server_url is empty.")
	}
	if payload.Key != m.secret {
		return m.state(machineID, payload.ServerURL, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License key secret is invalid.")
	}
	if len(payload.LicenseTypes) == 0 {
		return m.state(machineID, payload.ServerURL, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License type is empty.")
	}

	now := m.now().UTC()
	if payload.StartTime != nil && now.Before(*payload.StartTime) {
		return m.state(machineID, payload.ServerURL, key, StatusNotStarted, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License is not active yet.")
	}
	if payload.EndTime != nil && now.After(endOfDay(*payload.EndTime)) {
		return m.state(machineID, payload.ServerURL, key, StatusExpired, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License has expired.")
	}
	if machineID != "" && payload.ServerURL != machineID {
		return m.state(machineID, payload.ServerURL, key, StatusURLMismatch, entitlements.PlanBasic, payload.LicenseTypes, payload.StartTime, payload.EndTime, "License URL does not match the current dashboard URL.")
	}

	return m.state(machineID, payload.ServerURL, key, StatusActive, entitlements.PlanPro, payload.LicenseTypes, payload.StartTime, payload.EndTime, "Pro license is active.")
}

func (m *Manager) state(machineID, serverURL, key string, status Status, plan entitlements.Plan, licenseTypes []LicenseType, startTime, endTime *time.Time, message string) *State {
	return &State{
		MachineID:        machineID,
		ServerURL:        serverURL,
		Status:           status,
		Plan:             plan,
		LicenseKeyMasked: MaskLicenseKey(key),
		LicenseTypes:     append([]LicenseType(nil), licenseTypes...),
		Message:          message,
		StartTime:        startTime,
		EndTime:          endTime,
	}
}

func (m *Manager) readStoredLicense() (storedLicense, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.licensePath())
	if err != nil {
		if os.IsNotExist(err) {
			return storedLicense{}, false, nil
		}
		return storedLicense{}, false, fmt.Errorf("read license: %w", err)
	}

	var stored storedLicense
	if err := json.Unmarshal(data, &stored); err != nil {
		return storedLicense{
			Key: strings.TrimSpace(string(data)),
		}, true, nil
	}
	return stored, true, nil
}

func (m *Manager) writeStoredLicense(stored storedLicense) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureDatadir(); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal license: %w", err)
	}

	path := m.licensePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return fmt.Errorf("write license: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace license: %w", err)
	}
	return nil
}

func (m *Manager) deleteStoredLicense() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.Remove(m.licensePath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete license: %w", err)
	}
	return nil
}

func (m *Manager) ensureDatadir() error {
	if strings.TrimSpace(m.datadir) == "" {
		return errors.New("license datadir is empty")
	}
	if err := os.MkdirAll(m.datadir, 0700); err != nil {
		return fmt.Errorf("create license datadir: %w", err)
	}
	return nil
}

func (m *Manager) licensePath() string {
	return filepath.Join(m.datadir, licenseFileName)
}

func EncryptLicensePayload(rawPayload, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		secret = defaultSecret
	}

	block, err := aes.NewCipher(aesKey(secret))
	if err != nil {
		return "", fmt.Errorf("create license cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create license gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate license nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(rawPayload), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func DecryptLicensePayload(encoded, secret string) (*licensePayload, error) {
	plaintext, err := DecryptLicenseString(encoded, secret)
	if err != nil {
		return nil, err
	}
	return parseLicensePayload(plaintext)
}

func DecryptLicenseString(encoded, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		secret = defaultSecret
	}

	cipherData, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		cipherData, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return "", fmt.Errorf("decode license base64: %w", err)
		}
	}

	block, err := aes.NewCipher(aesKey(secret))
	if err != nil {
		return "", fmt.Errorf("create license cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create license gcm: %w", err)
	}
	if len(cipherData) <= gcm.NonceSize() {
		return "", errors.New("license payload is too short")
	}

	nonce := cipherData[:gcm.NonceSize()]
	ciphertext := cipherData[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt license: %w", err)
	}
	return string(plaintext), nil
}

func BuildLicensePayload(serverURL string, licenseTypes []LicenseType, secret string, startTime, endTime string) string {
	licenseValues := make([]string, 0, len(licenseTypes))
	for _, licenseType := range licenseTypes {
		licenseValues = append(licenseValues, string(licenseType))
	}
	return fmt.Sprintf(
		"server_url=%s,license=[%s],key=%s,start_time=%s,end_time=%s;",
		NormalizeServerURL(serverURL),
		strings.Join(licenseValues, ","),
		secret,
		startTime,
		endTime,
	)
}

func MaskLicenseKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 16 {
		return "****"
	}
	return key[:10] + "..." + key[len(key)-6:]
}

func NormalizeServerURL(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimSuffix(raw, "/")
	if raw == "" {
		return ""
	}

	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil {
			raw = parsed.Host
		}
	} else if strings.Contains(raw, "/") {
		raw = strings.Split(raw, "/")[0]
	}

	raw = strings.Trim(raw, "/")
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		raw = host
	}
	return strings.Trim(raw, "[]")
}

func parseLicensePayload(payload string) (*licensePayload, error) {
	payload = strings.TrimSpace(strings.TrimSuffix(payload, ";"))
	if payload == "" {
		return nil, errors.New("license payload is empty")
	}

	fields := map[string]string{}
	for _, part := range splitPayloadFields(payload) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid license field %q", part)
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	startTime, err := parseLicenseDate(fields["start_time"])
	if err != nil {
		return nil, err
	}
	endTime, err := parseLicenseDate(fields["end_time"])
	if err != nil {
		return nil, err
	}

	return &licensePayload{
		ServerURL:    NormalizeServerURL(fields["server_url"]),
		LicenseTypes: parseLicenseTypes(fields["license"]),
		Key:          fields["key"],
		StartTime:    startTime,
		EndTime:      endTime,
	}, nil
}

func splitPayloadFields(payload string) []string {
	var fields []string
	var current strings.Builder
	depth := 0
	for _, r := range payload {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				fields = append(fields, strings.TrimSpace(current.String()))
				current.Reset()
				continue
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		fields = append(fields, strings.TrimSpace(current.String()))
	}
	return fields
}

func parseLicenseTypes(value string) []LicenseType {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]LicenseType, 0, len(parts))
	seen := map[LicenseType]struct{}{}
	for _, part := range parts {
		licenseType := LicenseType(strings.ToLower(strings.TrimSpace(part)))
		switch licenseType {
		case LicenseTypeTrial, LicenseTypeYear, LicenseTypeEnterprise:
			if _, ok := seen[licenseType]; ok {
				continue
			}
			seen[licenseType] = struct{}{}
			result = append(result, licenseType)
		}
	}
	return result
}

func parseLicenseDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.ParseInLocation("2006/1/2", value, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("parse license date %q: %w", value, err)
	}
	return &parsed, nil
}

func endOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
}

func aesKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func licenseSecret() string {
	if secret := strings.TrimSpace(os.Getenv(licenseSecretEnv)); secret != "" {
		return secret
	}
	return defaultSecret
}

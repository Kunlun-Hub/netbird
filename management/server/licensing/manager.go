package licensing

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
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
	"unicode"
	"unicode/utf8"

	"github.com/netbirdio/netbird/management/server/entitlements"
)

const (
	licenseFileName = "license.json"

	licenseSecretEnv = "CLOINK_LICENSE_AES_KEY"
	defaultSecret    = "lA8fsCkh1s7e2JEruZCr0JNChQIfpuDbr6avPSbWgasz2cseGjNcZ225BAuCy4m2CDz8jMSHaQxHSWBXxfo1viFDDZRTDJqQ82oFfietnjhEYpuG1DPslfIpyFLiSvse"

	opensslSaltHeader = "Salted__"
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
	Name             string
	Status           Status
	Plan             entitlements.Plan
	LicenseKeyMasked string
	LicenseTypes     []LicenseType
	ResourceLimits   map[entitlements.Limit]int
	Usage            map[entitlements.Limit]int
	Message          string
	StartTime        *time.Time
	EndTime          *time.Time
	UpdatedAt        *time.Time
}

type licensePayload struct {
	ServerURL      string
	Name           string
	LicenseTypes   []LicenseType
	ResourceLimits map[entitlements.Limit]int
	Key            string
	StartTime      *time.Time
	EndTime        *time.Time
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
	applyResourceLimits(snapshot.Limits, state.ResourceLimits)
	snapshot.AccountID = accountID
	return snapshot, nil
}

func (m *Manager) MachineID(_ context.Context, serverURL string) (string, error) {
	currentServerURL := NormalizeServerURL(serverURL)
	if currentServerURL == "" {
		return "", errors.New("dashboard server URL is empty")
	}
	return m.machineIDForServerURL(currentServerURL)
}

func (m *Manager) GetState(ctx context.Context, serverURL string) (*State, error) {
	currentServerURL := NormalizeServerURL(serverURL)
	machineID, err := m.machineIDForServerURL(currentServerURL)
	if err != nil {
		return nil, err
	}

	stored, found, err := m.readStoredLicense()
	if err != nil {
		return nil, err
	}
	if !found || strings.TrimSpace(stored.Key) == "" {
		return m.state(machineID, "", "", "", StatusUnlicensed, entitlements.PlanBasic, nil, nil, nil, nil, "No license key has been installed."), nil
	}

	state := m.validate(ctx, currentServerURL, machineID, stored.Key)
	state.UpdatedAt = &stored.UpdatedAt
	return state, nil
}

func (m *Manager) UpdateKey(ctx context.Context, serverURL, key string) (*State, error) {
	currentServerURL := NormalizeServerURL(serverURL)
	if currentServerURL == "" {
		return nil, errors.New("dashboard server URL is empty")
	}
	machineID, err := m.machineIDForServerURL(currentServerURL)
	if err != nil {
		return nil, err
	}

	key = strings.TrimSpace(key)
	if key == "" {
		if err := m.deleteStoredLicense(); err != nil {
			return nil, err
		}
		return m.state(machineID, "", "", "", StatusUnlicensed, entitlements.PlanBasic, nil, nil, nil, nil, "No license key has been installed."), nil
	}

	state := m.validate(ctx, currentServerURL, machineID, key)
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

func (m *Manager) validate(_ context.Context, currentServerURL, machineID, key string) *State {
	payload, err := DecryptLicensePayload(key, m.secret)
	if err != nil {
		return m.state(machineID, "", "", key, StatusInvalid, entitlements.PlanBasic, nil, nil, nil, nil, "License key cannot be decrypted.")
	}

	payload.ServerURL = NormalizeServerURL(payload.ServerURL)
	if payload.ServerURL == "" {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License server_url is empty.")
	}
	if payload.Key != m.secret {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License key secret is invalid.")
	}
	if len(payload.LicenseTypes) == 0 {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusInvalid, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License type is empty.")
	}

	now := m.now().UTC()
	if payload.StartTime != nil && now.Before(*payload.StartTime) {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusNotStarted, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License is not active yet.")
	}
	if payload.EndTime != nil && now.After(endOfDay(*payload.EndTime)) {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusExpired, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License has expired.")
	}
	if currentServerURL != "" && payload.ServerURL != currentServerURL {
		return m.state(machineID, payload.ServerURL, payload.Name, key, StatusURLMismatch, entitlements.PlanBasic, payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License URL does not match the current dashboard URL.")
	}

	return m.state(machineID, payload.ServerURL, payload.Name, key, StatusActive, planFromLicenseTypes(payload.LicenseTypes), payload.LicenseTypes, payload.ResourceLimits, payload.StartTime, payload.EndTime, "License is active.")
}

func (m *Manager) machineIDForServerURL(serverURL string) (string, error) {
	serverURL = NormalizeServerURL(serverURL)
	if serverURL == "" {
		return "", nil
	}
	payload := BuildMachinePayload(serverURL, m.secret)
	return EncryptMachinePayload(payload, m.secret)
}

func (m *Manager) state(machineID, serverURL, name, key string, status Status, plan entitlements.Plan, licenseTypes []LicenseType, resourceLimits map[entitlements.Limit]int, startTime, endTime *time.Time, message string) *State {
	return &State{
		MachineID:        machineID,
		ServerURL:        serverURL,
		Name:             name,
		Status:           status,
		Plan:             plan,
		LicenseKeyMasked: MaskLicenseKey(key),
		LicenseTypes:     append([]LicenseType(nil), licenseTypes...),
		ResourceLimits:   cloneResourceLimits(resourceLimits),
		Usage:            map[entitlements.Limit]int{},
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

	salt := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate license salt: %w", err)
	}
	return encryptOpenSSLSaltedPayload(rawPayload, secret, salt)
}

func EncryptMachinePayload(rawPayload, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		secret = defaultSecret
	}

	sum := sha256.Sum256([]byte("cloink-machine-code:" + rawPayload))
	return encryptOpenSSLSaltedPayload(rawPayload, secret, sum[:8])
}

func encryptOpenSSLSaltedPayload(rawPayload, secret string, salt []byte) (string, error) {
	if len(salt) != 8 {
		return "", errors.New("license salt must be 8 bytes")
	}

	key, iv := opensslKeyIV([]byte(secret), salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create license cipher: %w", err)
	}

	plaintext := pkcs7Pad([]byte(rawPayload), aes.BlockSize)
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)

	payload := append([]byte(opensslSaltHeader), salt...)
	payload = append(payload, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
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

	if strings.HasPrefix(string(cipherData), opensslSaltHeader) {
		plaintext, err := decryptOpenSSLSaltedPayload(cipherData, secret)
		if err != nil {
			return "", err
		}
		return string(plaintext), nil
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

func decryptOpenSSLSaltedPayload(cipherData []byte, secret string) ([]byte, error) {
	if len(cipherData) <= len(opensslSaltHeader)+8 {
		return nil, errors.New("license payload is too short")
	}

	salt := cipherData[len(opensslSaltHeader) : len(opensslSaltHeader)+8]
	ciphertext := cipherData[len(opensslSaltHeader)+8:]
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("license ciphertext is not block aligned")
	}

	key, iv := opensslKeyIV([]byte(secret), salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create license cipher: %w", err)
	}

	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = pkcs7Unpad(plaintext, aes.BlockSize)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func opensslKeyIV(password, salt []byte) ([]byte, []byte) {
	const keyLength = 32
	const ivLength = aes.BlockSize
	result := make([]byte, 0, keyLength+ivLength)
	var previous []byte

	for len(result) < keyLength+ivLength {
		hash := md5.New()
		_, _ = hash.Write(previous)
		_, _ = hash.Write(password)
		_, _ = hash.Write(salt)
		previous = hash.Sum(nil)
		result = append(result, previous...)
	}

	return result[:keyLength], result[keyLength : keyLength+ivLength]
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, errors.New("invalid license padding")
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, errors.New("invalid license padding")
	}
	for _, value := range data[len(data)-padding:] {
		if int(value) != padding {
			return nil, errors.New("invalid license padding")
		}
	}
	return data[:len(data)-padding], nil
}

func BuildLicensePayload(serverURL string, licenseTypes []LicenseType, secret string, startTime, endTime string) string {
	return BuildLicensePayloadWithName(serverURL, licenseTypes, secret, startTime, endTime, "")
}

func BuildMachinePayload(serverURL, secret string) string {
	return fmt.Sprintf("server_url=%s,key=%s", NormalizeServerURL(serverURL), secret)
}

func BuildLicensePayloadWithName(serverURL string, licenseTypes []LicenseType, secret string, startTime, endTime, name string) string {
	licenseValues := make([]string, 0, len(licenseTypes))
	for _, licenseType := range licenseTypes {
		licenseValues = append(licenseValues, string(licenseType))
	}

	payload := fmt.Sprintf(
		"server_url=%s,license=[%s],key=%s,start_time=%s,end_time=%s",
		NormalizeServerURL(serverURL),
		strings.Join(licenseValues, ","),
		secret,
		startTime,
		endTime,
	)
	if strings.TrimSpace(name) != "" {
		payload += fmt.Sprintf(",name=%s", strings.TrimSpace(name))
	}
	return payload + ";"
}

func BuildLicensePayloadWithNameAndResources(serverURL string, licenseTypes []LicenseType, secret string, startTime, endTime, name string, resourceLimits map[entitlements.Limit]int) string {
	payload := strings.TrimSuffix(BuildLicensePayloadWithName(serverURL, licenseTypes, secret, startTime, endTime, name), ";")
	if resources := formatResourceLimits(resourceLimits); resources != "" {
		payload += fmt.Sprintf(",resources=[%s]", resources)
	}
	return payload + ";"
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
		ServerURL:      NormalizeServerURL(fields["server_url"]),
		Name:           parseLicenseName(fields["name"]),
		LicenseTypes:   parseLicenseTypes(fields["license"]),
		ResourceLimits: parseResourceLimits(fields["resources"]),
		Key:            fields["key"],
		StartTime:      startTime,
		EndTime:        endTime,
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

func parseLicenseName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
			continue
		}
		candidate := strings.TrimSpace(string(decoded))
		if candidate != "" && isPrintableString(candidate) {
			return candidate
		}
	}

	return value
}

func isPrintableString(value string) bool {
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func parseLicenseTypes(value string) []LicenseType {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return []LicenseType{LicenseTypeEnterprise}
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

func parseResourceLimits(value string) map[entitlements.Limit]int {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}

	limits := map[entitlements.Limit]int{}
	for _, part := range strings.Split(value, ",") {
		key, rawValue, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		limit := entitlements.Limit(strings.ToLower(strings.TrimSpace(key)))
		if !isKnownLimit(limit) {
			continue
		}
		var amount int
		if _, err := fmt.Sscanf(strings.TrimSpace(rawValue), "%d", &amount); err != nil || amount <= 0 {
			continue
		}
		limits[limit] = amount
	}
	if len(limits) == 0 {
		return nil
	}
	return limits
}

func isKnownLimit(limit entitlements.Limit) bool {
	for _, known := range entitlements.KnownLimits() {
		if limit == known {
			return true
		}
	}
	return false
}

func formatResourceLimits(limits map[entitlements.Limit]int) string {
	if len(limits) == 0 {
		return ""
	}
	parts := make([]string, 0, len(limits))
	for _, limit := range entitlements.KnownLimits() {
		if value := limits[limit]; value > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", limit, value))
		}
	}
	return strings.Join(parts, ",")
}

func cloneResourceLimits(limits map[entitlements.Limit]int) map[entitlements.Limit]int {
	if len(limits) == 0 {
		return nil
	}
	cloned := make(map[entitlements.Limit]int, len(limits))
	for limit, value := range limits {
		cloned[limit] = value
	}
	return cloned
}

func planFromLicenseTypes(licenseTypes []LicenseType) entitlements.Plan {
	for _, licenseType := range licenseTypes {
		if licenseType == LicenseTypeEnterprise {
			return entitlements.PlanPro
		}
	}
	return entitlements.PlanPro
}

func applyResourceLimits(limits map[entitlements.Limit]int, resourceLimits map[entitlements.Limit]int) {
	if len(limits) == 0 || len(resourceLimits) == 0 {
		return
	}
	for limit, value := range resourceLimits {
		if value > 0 {
			limits[limit] = value
		}
	}
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

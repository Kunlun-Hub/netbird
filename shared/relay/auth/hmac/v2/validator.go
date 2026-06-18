package v2

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const minLengthUnixTimestamp = 10

type Validator struct {
	secret []byte
}

type Claims struct {
	ExpiresAt           int64  `json:"exp"`
	AccountID           string `json:"account_id,omitempty"`
	RateLimitMbps       int    `json:"rate_limit_mbps,omitempty"`
	FairShareEnabled    bool   `json:"fair_share_enabled,omitempty"`
	RelayOnlyAccounting bool   `json:"relay_only_accounting,omitempty"`
}

func NewValidator(secret []byte) *Validator {
	return &Validator{secret: secret}
}

func (v *Validator) Validate(data any) error {
	_, err := v.ValidateWithClaims(data)
	return err
}

func (v *Validator) ValidateWithClaims(data any) (*Claims, error) {
	d, ok := data.([]byte)
	if !ok {
		return nil, fmt.Errorf("invalid data type")
	}

	token, err := UnmarshalToken(d)
	if err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	if len(token.Payload) < minLengthUnixTimestamp {
		return nil, errors.New("invalid payload: insufficient length")
	}

	hashFunc := token.AuthAlgo.New()
	if hashFunc == nil {
		return nil, fmt.Errorf("unsupported auth algorithm: %s", token.AuthAlgo)
	}

	h := hmac.New(hashFunc, v.secret)
	h.Write(token.Payload)
	expectedMAC := h.Sum(nil)

	if !hmac.Equal(token.Signature, expectedMAC) {
		return nil, errors.New("invalid signature")
	}

	claims, err := parseClaims(token.Payload)
	if err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("expired token")
	}

	return claims, nil
}

func parseClaims(payload []byte) (*Claims, error) {
	if len(payload) > 0 && payload[0] == '{' {
		var claims Claims
		if err := json.Unmarshal(payload, &claims); err != nil {
			return nil, err
		}
		if claims.ExpiresAt == 0 {
			return nil, errors.New("missing exp")
		}
		return &claims, nil
	}
	timestamp, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		return nil, err
	}
	return &Claims{ExpiresAt: timestamp}, nil
}

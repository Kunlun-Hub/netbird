package types

import "time"

const (
	SaaSOrganizationStatusActive    = "active"
	SaaSOrganizationStatusSuspended = "suspended"
	SaaSOrganizationStatusDeleted   = "deleted"

	SaaSSubscriptionStatusTrialing  = "trialing"
	SaaSSubscriptionStatusActive    = "active"
	SaaSSubscriptionStatusSuspended = "suspended"
	SaaSSubscriptionStatusCanceled  = "canceled"

	SaaSPlatformRoleAdmin    = "PlatformAdmin"
	SaaSPlatformRoleReadOnly = "PlatformReadOnly"

	SaaSTrafficSourceRelay    = "relay"
	SaaSTrafficSourceManual   = "manual"
	SaaSTrafficSourcePurchase = "purchase"

	SaaSTrafficTierHighSpeed = "high_speed"
	SaaSTrafficTierStandard  = "standard"

	SaaSTrafficPurchasePackageMonthly = "monthly"
	SaaSTrafficPurchasePackageQuarter = "quarterly"
	SaaSTrafficPurchasePackageYearly  = "yearly"
	SaaSTrafficPurchasePackageOneTime = "one_time"

	SaaSTrafficPurchaseStatusPending  = "pending"
	SaaSTrafficPurchaseStatusPaid     = "paid"
	SaaSTrafficPurchaseStatusApplied  = "applied"
	SaaSTrafficPurchaseStatusRefunded = "refunded"

	SaaSPaymentProviderAlipay = "alipay"

	SaaSPaymentOrderStatusPending  = "pending"
	SaaSPaymentOrderStatusPaid     = "paid"
	SaaSPaymentOrderStatusClosed   = "closed"
	SaaSPaymentOrderStatusRefunded = "refunded"
	SaaSPaymentOrderStatusFailed   = "failed"
)

type SaaSOrganization struct {
	AccountID   string `gorm:"primaryKey"`
	Slug        string `gorm:"uniqueIndex;size:63;not null"`
	Domain      string `gorm:"uniqueIndex;size:255;not null"`
	DisplayName string
	Status      string `gorm:"index;size:32;not null"`
	CreatedBy   string `gorm:"index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SaaSSubscription struct {
	AccountID             string `gorm:"primaryKey"`
	Plan                  string `gorm:"index;size:64;not null"`
	Status                string `gorm:"index;size:32;not null"`
	UsersLimit            int
	PeersLimit            int
	RelaysLimit           int
	HighSpeedTrafficBytes int64
	StandardRateLimitMbps int
	TotalRateLimitMbps    int
	Features              map[string]bool `gorm:"serializer:json"`
	TrialEndsAt           *time.Time
	ExpiresAt             *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type SaaSPlatformAdmin struct {
	UserID    string `gorm:"primaryKey"`
	Role      string `gorm:"index;size:64;not null"`
	Enabled   bool   `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SaaSOrgMenuVisibility struct {
	AccountID string `gorm:"primaryKey;size:255"`
	MenuKey   string `gorm:"primaryKey;size:128"`
	Visible   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SaaSTrafficLedger struct {
	ID         string `gorm:"primaryKey"`
	AccountID  string `gorm:"index;index:idx_saas_traffic_ledger_account_period,priority:1;not null"`
	PeriodKey  string `gorm:"index;index:idx_saas_traffic_ledger_account_period,priority:2;size:32;not null"`
	Source     string `gorm:"index;size:32;not null"`
	Direction  string `gorm:"size:32"`
	Bytes      int64
	Tier       string `gorm:"index;size:32;not null"`
	EventID    string `gorm:"uniqueIndex;size:255;not null"`
	RecordedAt time.Time
	CreatedAt  time.Time
}

type SaaSTrafficPurchase struct {
	ID                    string `gorm:"primaryKey"`
	AccountID             string `gorm:"index;not null"`
	PackageType           string `gorm:"index;size:64;not null"`
	HighSpeedTrafficBytes int64
	AmountCents           int64
	Currency              string `gorm:"size:16"`
	PaymentProvider       string `gorm:"index;size:32"`
	PaymentTradeNo        string `gorm:"index;size:255"`
	Status                string `gorm:"index;size:32;not null"`
	ValidFrom             *time.Time
	ValidUntil            *time.Time
	CreatedBy             string `gorm:"index"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type SaaSPaymentOrder struct {
	ID              string `gorm:"primaryKey"`
	AccountID       string `gorm:"index;not null"`
	Provider        string `gorm:"index;size:32;not null"`
	ProviderTradeNo string `gorm:"uniqueIndex;size:255"`
	PayURL          string
	Subject         string
	Body            string
	AmountCents     int64
	Currency        string         `gorm:"size:16"`
	Status          string         `gorm:"index;size:32;not null"`
	PurchaseID      string         `gorm:"index"`
	NotifyPayload   map[string]any `gorm:"serializer:json"`
	PaidAt          *time.Time
	CreatedBy       string `gorm:"index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SaaSBandwidthPolicy struct {
	AccountID              string `gorm:"primaryKey"`
	HighSpeedRateLimitMbps int
	StandardRateLimitMbps  int
	TotalRateLimitMbps     int
	RelayOnlyAccounting    bool
	FairShareEnabled       bool
	UpdatedAt              time.Time
}

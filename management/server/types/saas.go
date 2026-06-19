package types

import "time"

const (
	SaaSOrganizationStatusActive    = "active"
	SaaSOrganizationStatusSuspended = "suspended"
	SaaSOrganizationStatusDeleted   = "deleted"

	SaaSSubscriptionStatusTrialing  = "trialing"
	SaaSSubscriptionStatusActive    = "active"
	SaaSSubscriptionStatusPastDue   = "past_due"
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
	SaaSPaymentProviderWeChat = "wechat_pay"
	SaaSPaymentProviderBank   = "bank_transfer"

	SaaSPaymentOrderStatusPending  = "pending"
	SaaSPaymentOrderStatusPaid     = "paid"
	SaaSPaymentOrderStatusClosed   = "closed"
	SaaSPaymentOrderStatusRefunded = "refunded"
	SaaSPaymentOrderStatusFailed   = "failed"

	SaaSBillStatusOpen     = "open"
	SaaSBillStatusPaid     = "paid"
	SaaSBillStatusVoid     = "void"
	SaaSBillStatusRefunded = "refunded"

	SaaSBillItemTypeTrafficPackage = "traffic_package"

	SaaSPaymentRefundStatusSucceeded = "succeeded"
	SaaSPaymentRefundStatusFailed    = "failed"

	SaaSReconciliationStatusMatched  = "matched"
	SaaSReconciliationStatusMismatch = "mismatch"
	SaaSReconciliationStatusMissing  = "missing"

	SaaSInvoiceStatusRequested = "requested"
	SaaSInvoiceStatusIssued    = "issued"
	SaaSInvoiceStatusRejected  = "rejected"

	SaaSOfflinePaymentStatusPending  = "pending"
	SaaSOfflinePaymentStatusApplied  = "applied"
	SaaSOfflinePaymentStatusRejected = "rejected"

	SaaSAutoRenewalAttemptStatusSucceeded = "succeeded"
	SaaSAutoRenewalAttemptStatusFailed    = "failed"
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

type SaaSOrganizationDomain struct {
	Slug   string
	Domain string
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
	ID                    string     `gorm:"primaryKey" json:"id"`
	AccountID             string     `gorm:"index;not null" json:"account_id"`
	PackageType           string     `gorm:"index;size:64;not null" json:"package_type"`
	HighSpeedTrafficBytes int64      `json:"high_speed_traffic_bytes"`
	AmountCents           int64      `json:"amount_cents"`
	Currency              string     `gorm:"size:16" json:"currency"`
	PaymentProvider       string     `gorm:"index;size:32" json:"payment_provider,omitempty"`
	PaymentTradeNo        string     `gorm:"index;size:255" json:"payment_trade_no,omitempty"`
	Status                string     `gorm:"index;size:32;not null" json:"status"`
	ValidFrom             *time.Time `json:"valid_from,omitempty"`
	ValidUntil            *time.Time `json:"valid_until,omitempty"`
	CreatedBy             string     `gorm:"index" json:"created_by,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type SaaSPaymentOrder struct {
	ID              string         `gorm:"primaryKey" json:"id"`
	AccountID       string         `gorm:"index;not null" json:"account_id"`
	Provider        string         `gorm:"index;size:32;not null" json:"provider"`
	ProviderTradeNo string         `gorm:"uniqueIndex;size:255" json:"provider_trade_no,omitempty"`
	PayURL          string         `json:"pay_url,omitempty"`
	Subject         string         `json:"subject"`
	Body            string         `json:"body,omitempty"`
	AmountCents     int64          `json:"amount_cents"`
	Currency        string         `gorm:"size:16" json:"currency"`
	Status          string         `gorm:"index;size:32;not null" json:"status"`
	PurchaseID      string         `gorm:"index" json:"purchase_id,omitempty"`
	NotifyPayload   map[string]any `gorm:"serializer:json" json:"notify_payload,omitempty"`
	PaidAt          *time.Time     `json:"paid_at,omitempty"`
	CreatedBy       string         `gorm:"index" json:"created_by,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type SaaSBill struct {
	ID              string     `gorm:"primaryKey" json:"id"`
	AccountID       string     `gorm:"index;index:idx_saas_bills_account_period,priority:1;not null" json:"account_id"`
	PeriodKey       string     `gorm:"index;index:idx_saas_bills_account_period,priority:2;size:32;not null" json:"period_key"`
	Status          string     `gorm:"index;size:32;not null" json:"status"`
	SubtotalCents   int64      `json:"subtotal_cents"`
	DiscountCents   int64      `json:"discount_cents"`
	TaxCents        int64      `json:"tax_cents"`
	TotalCents      int64      `json:"total_cents"`
	Currency        string     `gorm:"size:16" json:"currency"`
	PaymentProvider string     `gorm:"index;size:32" json:"payment_provider,omitempty"`
	PaymentOrderID  string     `gorm:"uniqueIndex;size:255" json:"payment_order_id,omitempty"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
	DueAt           *time.Time `json:"due_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type SaaSBillItem struct {
	ID                    string    `gorm:"primaryKey" json:"id"`
	BillID                string    `gorm:"index;not null" json:"bill_id"`
	AccountID             string    `gorm:"index;not null" json:"account_id"`
	ItemType              string    `gorm:"index;size:64;not null" json:"item_type"`
	Description           string    `json:"description"`
	Quantity              int64     `json:"quantity"`
	UnitAmountCents       int64     `json:"unit_amount_cents"`
	AmountCents           int64     `json:"amount_cents"`
	Currency              string    `gorm:"size:16" json:"currency"`
	TrafficPurchaseID     string    `gorm:"index" json:"traffic_purchase_id,omitempty"`
	HighSpeedTrafficBytes int64     `json:"high_speed_traffic_bytes"`
	CreatedAt             time.Time `json:"created_at"`
}

type SaaSPaymentRefund struct {
	ID                    string         `gorm:"primaryKey" json:"id"`
	AccountID             string         `gorm:"index;not null" json:"account_id"`
	PaymentOrderID        string         `gorm:"index;not null" json:"payment_order_id"`
	Provider              string         `gorm:"index;size:32;not null" json:"provider"`
	ProviderTradeNo       string         `gorm:"index;size:255" json:"provider_trade_no,omitempty"`
	RefundTradeNo         string         `gorm:"uniqueIndex;size:255" json:"refund_trade_no"`
	AmountCents           int64          `json:"amount_cents"`
	Currency              string         `gorm:"size:16" json:"currency"`
	Reason                string         `json:"reason,omitempty"`
	Status                string         `gorm:"index;size:32;not null" json:"status"`
	HighSpeedTrafficBytes int64          `json:"high_speed_traffic_bytes"`
	OperatorID            string         `gorm:"index" json:"operator_id,omitempty"`
	Payload               map[string]any `gorm:"serializer:json" json:"payload,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type SaaSReconciliationRecord struct {
	ID                  string         `gorm:"primaryKey" json:"id"`
	AccountID           string         `gorm:"index" json:"account_id"`
	PaymentOrderID      string         `gorm:"index" json:"payment_order_id,omitempty"`
	Provider            string         `gorm:"index;size:32;not null" json:"provider"`
	ProviderTradeNo     string         `gorm:"index;size:255" json:"provider_trade_no,omitempty"`
	ExpectedAmountCents int64          `json:"expected_amount_cents"`
	ActualAmountCents   int64          `json:"actual_amount_cents"`
	Currency            string         `gorm:"size:16" json:"currency"`
	Status              string         `gorm:"index;size:32;not null" json:"status"`
	Reason              string         `json:"reason,omitempty"`
	OperatorID          string         `gorm:"index" json:"operator_id,omitempty"`
	RawPayload          map[string]any `gorm:"serializer:json" json:"raw_payload,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type SaaSInvoiceRequest struct {
	ID           string    `gorm:"primaryKey" json:"id"`
	AccountID    string    `gorm:"index;not null" json:"account_id"`
	BillID       string    `gorm:"index" json:"bill_id,omitempty"`
	InvoiceTitle string    `json:"invoice_title"`
	TaxID        string    `json:"tax_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	AmountCents  int64     `json:"amount_cents"`
	Currency     string    `gorm:"size:16" json:"currency"`
	Status       string    `gorm:"index;size:32;not null" json:"status"`
	InvoiceNo    string    `gorm:"index;size:128" json:"invoice_no,omitempty"`
	InvoiceURL   string    `json:"invoice_url,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	OperatorID   string    `gorm:"index" json:"operator_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SaaSOfflinePaymentRecord struct {
	ID                    string         `gorm:"primaryKey" json:"id"`
	AccountID             string         `gorm:"index;not null" json:"account_id"`
	Provider              string         `gorm:"index;size:32;not null" json:"provider"`
	ProviderTradeNo       string         `gorm:"index;size:255" json:"provider_trade_no,omitempty"`
	AmountCents           int64          `json:"amount_cents"`
	Currency              string         `gorm:"size:16" json:"currency"`
	Status                string         `gorm:"index;size:32;not null" json:"status"`
	Plan                  string         `json:"plan,omitempty"`
	PeriodDays            int            `json:"period_days,omitempty"`
	HighSpeedTrafficBytes int64          `json:"high_speed_traffic_bytes,omitempty"`
	PaymentOrderID        string         `gorm:"index" json:"payment_order_id,omitempty"`
	BillID                string         `gorm:"index" json:"bill_id,omitempty"`
	Note                  string         `json:"note,omitempty"`
	OperatorID            string         `gorm:"index" json:"operator_id,omitempty"`
	Payload               map[string]any `gorm:"serializer:json" json:"payload,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

type SaaSAutoRenewalAttempt struct {
	ID             string         `gorm:"primaryKey" json:"id"`
	AccountID      string         `gorm:"index;not null" json:"account_id"`
	Provider       string         `gorm:"index;size:32;not null" json:"provider"`
	AmountCents    int64          `json:"amount_cents"`
	Currency       string         `gorm:"size:16" json:"currency"`
	Status         string         `gorm:"index;size:32;not null" json:"status"`
	PaymentOrderID string         `gorm:"index" json:"payment_order_id,omitempty"`
	Reason         string         `json:"reason,omitempty"`
	NextRetryAt    *time.Time     `json:"next_retry_at,omitempty"`
	Payload        map[string]any `gorm:"serializer:json" json:"payload,omitempty"`
	AttemptedAt    time.Time      `json:"attempted_at"`
	CreatedAt      time.Time      `json:"created_at"`
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

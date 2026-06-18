package saas

import (
	"context"
	"time"

	"github.com/rs/xid"

	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type TrafficService struct {
	Store store.Store
}

type TrafficUsage struct {
	AccountID               string `json:"account_id"`
	PeriodKey               string `json:"period_key"`
	HighSpeedTrafficBytes   int64  `json:"high_speed_traffic_bytes"`
	HighSpeedUsedBytes      int64  `json:"high_speed_used_bytes"`
	HighSpeedRemainingBytes int64  `json:"high_speed_remaining_bytes"`
	TrafficTier             string `json:"traffic_tier"`
	HighSpeedRateLimitMbps  int    `json:"high_speed_rate_limit_mbps"`
	StandardRateLimitMbps   int    `json:"standard_rate_limit_mbps"`
	TotalRateLimitMbps      int    `json:"total_rate_limit_mbps"`
	EffectiveRateLimitMbps  int    `json:"effective_rate_limit_mbps"`
	RelayOnlyAccounting     bool   `json:"relay_only_accounting"`
	FairShareEnabled        bool   `json:"fair_share_enabled"`
}

type RelayTrafficEvent struct {
	AccountID  string
	EventID    string
	Bytes      int64
	Direction  string
	RecordedAt time.Time
}

type TrafficPurchaseRequest struct {
	AccountID             string
	PurchaseID            string
	PackageType           string
	HighSpeedTrafficBytes int64
	CreatedBy             string
	ValidFrom             *time.Time
	ValidUntil            *time.Time
}

func (s TrafficService) ApplyTrafficPurchase(ctx context.Context, req TrafficPurchaseRequest) (*types.SaaSTrafficPurchase, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if req.AccountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if req.HighSpeedTrafficBytes <= 0 {
		return nil, status.Errorf(status.InvalidArgument, "high speed traffic bytes must be positive")
	}
	if req.PackageType == "" {
		req.PackageType = types.SaaSTrafficPurchasePackageOneTime
	}
	if req.PurchaseID == "" {
		req.PurchaseID = xid.New().String()
	}

	now := time.Now().UTC()
	purchase := &types.SaaSTrafficPurchase{
		ID:                    req.PurchaseID,
		AccountID:             req.AccountID,
		PackageType:           req.PackageType,
		HighSpeedTrafficBytes: req.HighSpeedTrafficBytes,
		Status:                types.SaaSTrafficPurchaseStatusApplied,
		ValidFrom:             req.ValidFrom,
		ValidUntil:            req.ValidUntil,
		CreatedBy:             req.CreatedBy,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	err := s.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		subscription, err := tx.GetSaaSSubscription(ctx, store.LockingStrengthUpdate, req.AccountID)
		if err != nil {
			return err
		}
		subscription.HighSpeedTrafficBytes += req.HighSpeedTrafficBytes
		subscription.UpdatedAt = now
		if err := tx.SaveSaaSSubscription(ctx, subscription); err != nil {
			return err
		}
		return tx.SaveSaaSTrafficPurchase(ctx, purchase)
	})
	if err != nil {
		return nil, err
	}
	return purchase, nil
}

func (s TrafficService) RecordRelayTraffic(ctx context.Context, event RelayTrafficEvent) error {
	if s.Store == nil {
		return status.Errorf(status.Internal, "store is required")
	}
	if event.AccountID == "" {
		return status.Errorf(status.InvalidArgument, "account id is required")
	}
	if event.EventID == "" {
		return status.Errorf(status.InvalidArgument, "event id is required")
	}
	if event.Bytes <= 0 {
		return status.Errorf(status.InvalidArgument, "traffic bytes must be positive")
	}
	recordedAt := event.RecordedAt.UTC()
	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}
	if event.Direction == "" {
		event.Direction = "both"
	}

	usage, err := s.Usage(ctx, event.AccountID, recordedAt)
	if err != nil {
		return err
	}
	tier := usage.TrafficTier

	return s.Store.CreateSaaSTrafficLedger(ctx, &types.SaaSTrafficLedger{
		ID:         xid.New().String(),
		AccountID:  event.AccountID,
		PeriodKey:  recordedAt.Format("2006-01"),
		Source:     types.SaaSTrafficSourceRelay,
		Direction:  event.Direction,
		Bytes:      event.Bytes,
		Tier:       tier,
		EventID:    event.EventID,
		RecordedAt: recordedAt,
		CreatedAt:  time.Now().UTC(),
	})
}

func (s TrafficService) Usage(ctx context.Context, accountID string, now time.Time) (*TrafficUsage, error) {
	if s.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if accountID == "" {
		return nil, status.Errorf(status.InvalidArgument, "account id is required")
	}
	if _, err := s.Store.GetSaaSOrganizationByAccountID(ctx, store.LockingStrengthNone, accountID); err != nil {
		return nil, err
	}
	subscription, err := s.Store.GetSaaSSubscription(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	policy, err := s.Store.GetSaaSBandwidthPolicy(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}

	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	periodKey := now.Format("2006-01")
	entries, err := s.Store.GetSaaSTrafficLedgerForPeriod(ctx, store.LockingStrengthNone, accountID, periodKey)
	if err != nil {
		return nil, err
	}
	var used int64
	for _, entry := range entries {
		if entry.Tier == types.SaaSTrafficTierHighSpeed {
			used += entry.Bytes
		}
	}

	remaining := subscription.HighSpeedTrafficBytes - used
	if remaining < 0 {
		remaining = 0
	}
	tier := types.SaaSTrafficTierHighSpeed
	if subscription.HighSpeedTrafficBytes > 0 && remaining == 0 {
		tier = types.SaaSTrafficTierStandard
	}
	effectiveRate := effectiveRelayRateLimitMbps(policy, tier)

	return &TrafficUsage{
		AccountID:               accountID,
		PeriodKey:               periodKey,
		HighSpeedTrafficBytes:   subscription.HighSpeedTrafficBytes,
		HighSpeedUsedBytes:      used,
		HighSpeedRemainingBytes: remaining,
		TrafficTier:             tier,
		HighSpeedRateLimitMbps:  policy.HighSpeedRateLimitMbps,
		StandardRateLimitMbps:   policy.StandardRateLimitMbps,
		TotalRateLimitMbps:      policy.TotalRateLimitMbps,
		EffectiveRateLimitMbps:  effectiveRate,
		RelayOnlyAccounting:     policy.RelayOnlyAccounting,
		FairShareEnabled:        policy.FairShareEnabled,
	}, nil
}

func effectiveRelayRateLimitMbps(policy *types.SaaSBandwidthPolicy, tier string) int {
	if policy == nil {
		return 0
	}
	tierRate := policy.HighSpeedRateLimitMbps
	if tier == types.SaaSTrafficTierStandard {
		tierRate = policy.StandardRateLimitMbps
	}
	if policy.TotalRateLimitMbps <= 0 {
		return tierRate
	}
	if tierRate <= 0 || policy.TotalRateLimitMbps < tierRate {
		return policy.TotalRateLimitMbps
	}
	return tierRate
}

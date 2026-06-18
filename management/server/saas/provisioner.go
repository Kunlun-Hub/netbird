package saas

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/xid"

	nbdns "github.com/netbirdio/netbird/dns"
	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/route"
	"github.com/netbirdio/netbird/shared/management/status"
)

var DefaultMenuKeys = []string{
	"routes",
	"dns",
	"relays",
	"workbench",
	"reverse_proxy",
}

type Provisioner struct {
	Store  store.Store
	Config nbconfig.SaaSConfig
}

type SignupRequest struct {
	Email            string
	Password         string
	Name             string
	OrganizationName string
}

type SignupResult struct {
	AccountID          string
	UserID             string
	OrganizationSlug   string
	OrganizationDomain string
	DashboardURL       string
}

func (p Provisioner) ProvisionOrganization(ctx context.Context, userID string, req SignupRequest) (*SignupResult, error) {
	if p.Store == nil {
		return nil, status.Errorf(status.Internal, "store is required")
	}
	if !p.Config.Enabled || !p.Config.PublicSignupEnabled {
		return nil, status.Errorf(status.PermissionDenied, "public signup is disabled")
	}
	if userID == "" {
		return nil, status.Errorf(status.InvalidArgument, "user id is required")
	}
	if err := validateSignupRequest(req); err != nil {
		return nil, err
	}

	config := p.Config
	config.ApplyDefaults()
	allocated, err := (DomainAllocator{
		Store:  p.Store,
		Suffix: config.OrganizationDomainSuffix,
	}).Allocate(ctx)
	if err != nil {
		return nil, err
	}

	accountID := xid.New().String()
	now := time.Now().UTC()
	result := &SignupResult{
		AccountID:          accountID,
		UserID:             userID,
		OrganizationSlug:   allocated.Slug,
		OrganizationDomain: allocated.Domain,
		DashboardURL:       "https://" + allocated.Domain,
	}

	err = p.Store.ExecuteInTransaction(ctx, func(tx store.Store) error {
		account := newSaaSAccount(accountID, userID, allocated.Domain, req.Email, req.Name, now)
		if err := tx.SaveAccount(ctx, account); err != nil {
			return err
		}
		if err := tx.SaveSaaSOrganization(ctx, &types.SaaSOrganization{
			AccountID:   accountID,
			Slug:        allocated.Slug,
			Domain:      allocated.Domain,
			DisplayName: req.OrganizationName,
			Status:      types.SaaSOrganizationStatusActive,
			CreatedBy:   userID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			return err
		}
		if err := tx.SaveSaaSSubscription(ctx, defaultSubscription(accountID, config, now)); err != nil {
			return err
		}
		if err := tx.SaveSaaSBandwidthPolicy(ctx, defaultBandwidthPolicy(accountID, config, now)); err != nil {
			return err
		}
		for _, menuKey := range DefaultMenuKeys {
			if err := tx.SaveSaaSOrgMenuVisibility(ctx, &types.SaaSOrgMenuVisibility{
				AccountID: accountID,
				MenuKey:   menuKey,
				Visible:   true,
				CreatedAt: now,
				UpdatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func validateSignupRequest(req SignupRequest) error {
	if !strings.Contains(req.Email, "@") {
		return status.Errorf(status.InvalidArgument, "valid email is required")
	}
	if len(req.Password) < 8 {
		return status.Errorf(status.InvalidArgument, "password must be at least 8 characters")
	}
	if strings.TrimSpace(req.Name) == "" {
		return status.Errorf(status.InvalidArgument, "name is required")
	}
	if strings.TrimSpace(req.OrganizationName) == "" {
		return status.Errorf(status.InvalidArgument, "organization name is required")
	}
	return nil
}

func newSaaSAccount(accountID, userID, domain, email, name string, now time.Time) *types.Account {
	owner := types.NewOwnerUser(userID, email, name)
	owner.AccountID = accountID
	owner.CreatedAt = now

	account := &types.Account{
		Id:               accountID,
		CreatedBy:        userID,
		CreatedAt:        now,
		Domain:           domain,
		DomainCategory:   types.PrivateCategory,
		SetupKeys:        map[string]*types.SetupKey{},
		Network:          types.NewNetwork(),
		Peers:            map[string]*nbpeer.Peer{},
		Users:            map[string]*types.User{userID: owner},
		Groups:           map[string]*types.Group{},
		Routes:           map[route.ID]*route.Route{},
		NameServerGroups: map[string]*nbdns.NameServerGroup{},
		DNSSettings: types.DNSSettings{
			DisabledManagementGroups: []string{},
		},
		Settings: &types.Settings{
			PeerLoginExpirationEnabled:      true,
			PeerLoginExpiration:             types.DefaultPeerLoginExpiration,
			GroupsPropagationEnabled:        true,
			RegularUsersViewBlocked:         true,
			PeerInactivityExpirationEnabled: false,
			PeerInactivityExpiration:        types.DefaultPeerInactivityExpiration,
			RoutingPeerDNSResolutionEnabled: true,
			DNSDomain:                       domain,
			Extra: &types.ExtraSettings{
				UserApprovalRequired: true,
			},
		},
		Onboarding: types.AccountOnboarding{
			AccountID:             accountID,
			OnboardingFlowPending: true,
			SignupFormPending:     true,
			CreatedAt:             now,
			UpdatedAt:             now,
		},
	}
	if err := account.AddAllGroup(false); err != nil {
		panic(fmt.Sprintf("failed to add all group to SaaS account: %v", err))
	}
	if allGroup, err := account.GetGroupAll(); err == nil {
		allGroup.AccountID = accountID
		account.Settings.IPv6EnabledGroups = []string{allGroup.ID}
	}
	for _, policy := range account.Policies {
		policy.AccountID = accountID
	}
	return account
}

func defaultSubscription(accountID string, config nbconfig.SaaSConfig, now time.Time) *types.SaaSSubscription {
	return &types.SaaSSubscription{
		AccountID:             accountID,
		Plan:                  config.DefaultPlan,
		Status:                types.SaaSSubscriptionStatusActive,
		UsersLimit:            config.DefaultUsersLimit,
		PeersLimit:            config.DefaultPeersLimit,
		RelaysLimit:           config.DefaultRelaysLimit,
		HighSpeedTrafficBytes: int64(config.DefaultHighSpeedTrafficGB) << 30,
		StandardRateLimitMbps: config.DefaultStandardRateLimitMbps,
		TotalRateLimitMbps:    config.DefaultTotalRateLimitMbps,
		Features:              map[string]bool{},
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func defaultBandwidthPolicy(accountID string, config nbconfig.SaaSConfig, now time.Time) *types.SaaSBandwidthPolicy {
	return &types.SaaSBandwidthPolicy{
		AccountID:              accountID,
		HighSpeedRateLimitMbps: config.DefaultTotalRateLimitMbps,
		StandardRateLimitMbps:  config.DefaultStandardRateLimitMbps,
		TotalRateLimitMbps:     config.DefaultTotalRateLimitMbps,
		RelayOnlyAccounting:    config.ForceRelayForTrafficBilling,
		FairShareEnabled:       true,
		UpdatedAt:              now,
	}
}

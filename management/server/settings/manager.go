package settings

//go:generate go run github.com/golang/mock/mockgen -package settings -destination=manager_mock.go -source=./manager.go -build_flags=-mod=mod

import (
	"context"
	"fmt"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/integrations/extra_settings"
	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/management/server/users"
	"github.com/netbirdio/netbird/shared/management/status"
)

type Manager interface {
	GetExtraSettingsManager() extra_settings.Manager
	GetSettings(ctx context.Context, accountID string, userID string) (*types.Settings, error)
	GetExtraSettings(ctx context.Context, accountID string) (*types.ExtraSettings, error)
	UpdateExtraSettings(ctx context.Context, accountID, userID string, extraSettings *types.ExtraSettings) (bool, error)
}

// IdpConfig holds IdP-related configuration that is set at runtime
// and not stored in the database.
type IdpConfig struct {
	EmbeddedIdpEnabled bool
	LocalAuthDisabled  bool
}

type managerImpl struct {
	store                store.Store
	extraSettingsManager extra_settings.Manager
	userManager          users.Manager
	permissionsManager   permissions.Manager
	idpConfig            IdpConfig
}

func NewManager(store store.Store, userManager users.Manager, extraSettingsManager extra_settings.Manager, permissionsManager permissions.Manager, idpConfig IdpConfig) Manager {
	return &managerImpl{
		store:                store,
		extraSettingsManager: extraSettingsManager,
		userManager:          userManager,
		permissionsManager:   permissionsManager,
		idpConfig:            idpConfig,
	}
}

func (m *managerImpl) GetExtraSettingsManager() extra_settings.Manager {
	return m.extraSettingsManager
}

func (m *managerImpl) GetSettings(ctx context.Context, accountID, userID string) (*types.Settings, error) {
	if userID != activity.SystemInitiator {
		ok, err := m.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operations.Read)
		if err != nil {
			return nil, status.NewPermissionValidationError(err)
		}
		if !ok {
			return nil, status.NewPermissionDeniedError()
		}
	}

	extraSettings, err := m.extraSettingsManager.GetExtraSettings(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get extra settings: %w", err)
	}

	settings, err := m.store.GetAccountSettings(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account settings: %w", err)
	}

	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}

	mergeFlowExtraSettings(settings.Extra, extraSettings)

	// Fill in IdP-related runtime settings
	settings.EmbeddedIdpEnabled = m.idpConfig.EmbeddedIdpEnabled
	settings.LocalAuthDisabled = m.idpConfig.LocalAuthDisabled

	return settings, nil
}

func (m *managerImpl) GetExtraSettings(ctx context.Context, accountID string) (*types.ExtraSettings, error) {
	extraSettings, err := m.extraSettingsManager.GetExtraSettings(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get extra settings: %w", err)
	}

	settings, err := m.store.GetAccountSettings(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, fmt.Errorf("get account settings: %w", err)
	}

	if settings.Extra == nil {
		settings.Extra = &types.ExtraSettings{}
	}

	mergeFlowExtraSettings(settings.Extra, extraSettings)

	return settings.Extra, nil
}

func (m *managerImpl) UpdateExtraSettings(ctx context.Context, accountID, userID string, extraSettings *types.ExtraSettings) (bool, error) {
	return m.extraSettingsManager.UpdateExtraSettings(ctx, accountID, userID, extraSettings)
}

func mergeFlowExtraSettings(target, source *types.ExtraSettings) {
	if target == nil || source == nil {
		return
	}

	// Persisted account settings are the source of truth for the dashboard.
	// If an external extra settings manager provides explicit flow values, let them enrich runtime config.
	if source.FlowEnabled {
		target.FlowEnabled = true
	}
	if len(source.FlowGroups) > 0 {
		target.FlowGroups = source.FlowGroups
	}
	if source.FlowPacketCounterEnabled {
		target.FlowPacketCounterEnabled = true
	}
	if source.FlowENCollectionEnabled {
		target.FlowENCollectionEnabled = true
	}
	if source.FlowDnsCollectionEnabled {
		target.FlowDnsCollectionEnabled = true
	}
}

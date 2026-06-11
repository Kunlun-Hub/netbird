package workbench

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rs/xid"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	nbtypes "github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/management/server/workbench/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

type Store interface {
	GetUserByUserID(ctx context.Context, lockStrength store.LockingStrength, userID string) (*nbtypes.User, error)
	GetGroupsByIDs(ctx context.Context, lockStrength store.LockingStrength, accountID string, groupIDs []string) (map[string]*nbtypes.Group, error)
	GetWorkbenchServerResources(ctx context.Context, accountID string) ([]*types.Resource, error)
	GetWorkbenchServerResource(ctx context.Context, accountID, resourceID string) (*types.Resource, error)
	SaveWorkbenchServerResource(ctx context.Context, resource *types.Resource) error
	DeleteWorkbenchServerResource(ctx context.Context, accountID, resourceID string) error
	GetWorkbenchUserState(ctx context.Context, accountID, userID string) (*types.UserResources, error)
	GetWorkbenchUserResources(ctx context.Context, accountID, userID string) ([]types.Resource, error)
	SaveWorkbenchUserResources(ctx context.Context, accountID, userID string, resources []types.Resource) error
	SaveWorkbenchUserRecentVisits(ctx context.Context, accountID, userID string, recentVisits []types.RecentVisit) error
	SaveWorkbenchAsset(ctx context.Context, asset *types.Asset) error
	GetWorkbenchAsset(ctx context.Context, accountID, assetID string) (*types.Asset, error)
}

type PermissionsManager interface {
	ValidateUserPermissions(ctx context.Context, accountID, userID string, module modules.Module, operation operations.Operation) (bool, context.Context, error)
}

type EventStore interface {
	StoreEvent(ctx context.Context, initiatorID, targetID, accountID string, activityID activity.ActivityDescriber, meta map[string]any)
}

type Manager interface {
	ListResources(ctx context.Context, accountID, userID string) (*types.ResourceList, error)
	ListAdminResources(ctx context.Context, accountID, userID string) ([]*types.Resource, error)
	GetAdminResource(ctx context.Context, accountID, userID, resourceID string) (*types.Resource, error)
	CreateAdminResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error)
	UpdateAdminResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error)
	DeleteAdminResource(ctx context.Context, accountID, userID, resourceID string) error
	CreatePersonalResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error)
	UpdatePersonalResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error)
	DeletePersonalResource(ctx context.Context, accountID, userID, resourceID string) error
	EnsureAdminAssetAccess(ctx context.Context, accountID, userID string) error
	EnsurePersonalAssetAccess(ctx context.Context, accountID, userID string) error
	SaveAdminIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error)
	SavePersonalIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error)
	GetAsset(ctx context.Context, accountID, userID, assetID string) (*types.Asset, error)
	RecordLaunch(ctx context.Context, accountID, userID, resourceID, scope string) error
}

type manager struct {
	store              Store
	permissionsManager PermissionsManager
	eventStore         EventStore
}

func NewManager(store Store, permissionsManager PermissionsManager) Manager {
	return NewManagerWithEvents(store, permissionsManager, nil)
}

func NewManagerWithEvents(store Store, permissionsManager PermissionsManager, eventStore EventStore) Manager {
	return &manager{store: store, permissionsManager: permissionsManager, eventStore: eventStore}
}

func (m *manager) ListResources(ctx context.Context, accountID, userID string) (*types.ResourceList, error) {
	user, err := m.getAccountUser(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}

	serverResources, err := m.store.GetWorkbenchServerResources(ctx, accountID)
	if err != nil {
		return nil, err
	}

	visibleServer := make([]types.Resource, 0, len(serverResources))
	for _, resource := range serverResources {
		if resource == nil {
			continue
		}
		if resource.Enabled && isVisibleToUser(resource, user) {
			normalized := normalizeServerResourceForClient(accountID, *resource)
			visibleServer = append(visibleServer, publicResourceForClient(normalized))
		}
	}
	sortResources(visibleServer)

	personalResources, err := m.store.GetWorkbenchUserResources(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	normalizedPersonalResources := normalizePersonalResources(accountID, userID, personalResources)
	if err := m.applyRecentVisits(ctx, accountID, userID, visibleServer, normalizedPersonalResources); err != nil {
		return nil, err
	}
	sortResources(normalizedPersonalResources)

	return &types.ResourceList{
		ServerResources:   visibleServer,
		PersonalResources: publicResourcesForClient(normalizedPersonalResources),
		Categories:        defaultCategories(visibleServer, normalizedPersonalResources),
		Version:           1,
	}, nil
}

func (m *manager) ListAdminResources(ctx context.Context, accountID, userID string) ([]*types.Resource, error) {
	if err := m.ensureAdminAccess(ctx, accountID, userID, operations.Read); err != nil {
		return nil, err
	}
	resources, err := m.store.GetWorkbenchServerResources(ctx, accountID)
	if err != nil {
		return nil, err
	}
	normalized := make([]*types.Resource, 0, len(resources))
	for _, resource := range resources {
		if resource == nil {
			continue
		}
		next := normalizeServerResourceForAdmin(accountID, *resource)
		normalized = append(normalized, &next)
	}
	return normalized, nil
}

func (m *manager) GetAdminResource(ctx context.Context, accountID, userID, resourceID string) (*types.Resource, error) {
	if err := m.ensureAdminAccess(ctx, accountID, userID, operations.Read); err != nil {
		return nil, err
	}
	resourceID, err := normalizeResourceID(resourceID)
	if err != nil {
		return nil, err
	}
	resource, err := m.store.GetWorkbenchServerResource(ctx, accountID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, status.Errorf(status.NotFound, "workbench resource not found")
	}
	normalized := normalizeServerResourceForAdmin(accountID, *resource)
	return &normalized, nil
}

func (m *manager) CreateAdminResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error) {
	if err := m.ensureAdminAccess(ctx, accountID, userID, operations.Create); err != nil {
		return nil, err
	}
	if err := ensureResourcePayload(resource); err != nil {
		return nil, err
	}
	resource.ID = ""
	normalizeServerResource(accountID, userID, resource)
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	if err := m.validateResourceVisibility(ctx, accountID, resource); err != nil {
		return nil, err
	}
	if err := m.store.SaveWorkbenchServerResource(ctx, resource); err != nil {
		return nil, err
	}
	created, err := m.store.GetWorkbenchServerResource(ctx, accountID, resource.ID)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, status.Errorf(status.Internal, "failed to load saved workbench resource")
	}
	return created, nil
}

func (m *manager) UpdateAdminResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error) {
	if err := m.ensureAdminAccess(ctx, accountID, userID, operations.Update); err != nil {
		return nil, err
	}
	if err := ensureResourcePayload(resource); err != nil {
		return nil, err
	}
	resourceID, err := normalizeResourceID(resourceID)
	if err != nil {
		return nil, err
	}
	existing, err := m.store.GetWorkbenchServerResource(ctx, accountID, resourceID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, status.Errorf(status.NotFound, "workbench resource not found")
	}
	resource.ID = resourceID
	normalizeServerResource(accountID, userID, resource)
	resource.CreatedBy = existing.CreatedBy
	resource.CreatedAt = existing.CreatedAt
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	if err := m.validateResourceVisibility(ctx, accountID, resource); err != nil {
		return nil, err
	}
	if err := m.store.SaveWorkbenchServerResource(ctx, resource); err != nil {
		return nil, err
	}
	updated, err := m.store.GetWorkbenchServerResource(ctx, accountID, resourceID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, status.Errorf(status.Internal, "failed to load saved workbench resource")
	}
	return updated, nil
}

func (m *manager) DeleteAdminResource(ctx context.Context, accountID, userID, resourceID string) error {
	if err := m.ensureAdminAccess(ctx, accountID, userID, operations.Delete); err != nil {
		return err
	}
	resourceID, err := normalizeResourceID(resourceID)
	if err != nil {
		return err
	}
	return m.store.DeleteWorkbenchServerResource(ctx, accountID, resourceID)
}

func (m *manager) CreatePersonalResource(ctx context.Context, accountID, userID string, resource *types.Resource) (*types.Resource, error) {
	if _, err := m.getAccountUser(ctx, accountID, userID); err != nil {
		return nil, err
	}
	if err := ensureResourcePayload(resource); err != nil {
		return nil, err
	}
	resources, err := m.store.GetWorkbenchUserResources(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	resource.ID = ""
	normalizePersonalResource(accountID, userID, resource)
	resource.Enabled = true
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	resources = append(resources, *resource)
	if err := m.store.SaveWorkbenchUserResources(ctx, accountID, userID, resources); err != nil {
		return nil, err
	}
	return resource, nil
}

func (m *manager) UpdatePersonalResource(ctx context.Context, accountID, userID, resourceID string, resource *types.Resource) (*types.Resource, error) {
	if _, err := m.getAccountUser(ctx, accountID, userID); err != nil {
		return nil, err
	}
	if err := ensureResourcePayload(resource); err != nil {
		return nil, err
	}
	resourceID, err := normalizeResourceID(resourceID)
	if err != nil {
		return nil, err
	}
	resources, err := m.store.GetWorkbenchUserResources(ctx, accountID, userID)
	if err != nil {
		return nil, err
	}
	normalizePersonalResource(accountID, userID, resource)
	resource.ID = resourceID
	resource.Enabled = true
	if err := validateResource(resource); err != nil {
		return nil, err
	}
	for i := range resources {
		if resources[i].ID == resourceID {
			resources[i] = *resource
			if err := m.store.SaveWorkbenchUserResources(ctx, accountID, userID, resources); err != nil {
				return nil, err
			}
			return resource, nil
		}
	}
	return nil, status.Errorf(status.NotFound, "personal workbench resource not found")
}

func (m *manager) DeletePersonalResource(ctx context.Context, accountID, userID, resourceID string) error {
	if _, err := m.getAccountUser(ctx, accountID, userID); err != nil {
		return err
	}
	resourceID, err := normalizeResourceID(resourceID)
	if err != nil {
		return err
	}
	resources, err := m.store.GetWorkbenchUserResources(ctx, accountID, userID)
	if err != nil {
		return err
	}
	for i := range resources {
		if resources[i].ID == resourceID {
			resources = append(resources[:i], resources[i+1:]...)
			return m.store.SaveWorkbenchUserResources(ctx, accountID, userID, resources)
		}
	}
	return status.Errorf(status.NotFound, "personal workbench resource not found")
}

func (m *manager) SaveAdminIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error) {
	if err := m.EnsureAdminAssetAccess(ctx, accountID, userID); err != nil {
		return nil, err
	}
	if assetID == "" {
		assetID = xid.New().String()
	}
	asset := &types.Asset{
		ID:          assetID,
		AccountID:   accountID,
		OwnerUserID: "",
		SourceURL:   sourceURL,
		PublicURL:   publicURL,
		StoragePath: storagePath,
		ContentType: contentType,
		Size:        size,
		SHA256:      sha256,
	}
	if err := m.store.SaveWorkbenchAsset(ctx, asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (m *manager) SavePersonalIconAsset(ctx context.Context, accountID, userID, assetID, sourceURL, publicURL, storagePath, contentType, sha256 string, size int64) (*types.Asset, error) {
	if _, err := m.getAccountUser(ctx, accountID, userID); err != nil {
		return nil, err
	}
	if assetID == "" {
		assetID = xid.New().String()
	}
	asset := &types.Asset{
		ID:          assetID,
		AccountID:   accountID,
		OwnerUserID: userID,
		SourceURL:   sourceURL,
		PublicURL:   publicURL,
		StoragePath: storagePath,
		ContentType: contentType,
		Size:        size,
		SHA256:      sha256,
	}
	if err := m.store.SaveWorkbenchAsset(ctx, asset); err != nil {
		return nil, err
	}
	return asset, nil
}

func (m *manager) GetAsset(ctx context.Context, accountID, userID, assetID string) (*types.Asset, error) {
	if _, err := m.getAccountUser(ctx, accountID, userID); err != nil {
		return nil, err
	}
	assetID, err := normalizeAssetID(assetID)
	if err != nil {
		return nil, err
	}
	asset, err := m.store.GetWorkbenchAsset(ctx, accountID, assetID)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, status.Errorf(status.NotFound, "workbench asset not found")
	}
	if asset.AccountID != accountID {
		return nil, status.Errorf(status.NotFound, "workbench asset not found")
	}
	if asset.OwnerUserID != "" && asset.OwnerUserID != userID {
		return nil, status.Errorf(status.NotFound, "workbench asset not found")
	}
	return asset, nil
}

func (m *manager) RecordLaunch(ctx context.Context, accountID, userID, resourceID, scope string) error {
	user, err := m.getAccountUser(ctx, accountID, userID)
	if err != nil {
		return err
	}
	resourceID, err = normalizeResourceID(resourceID)
	if err != nil {
		return err
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return status.Errorf(status.InvalidArgument, "resource scope shouldn't be empty")
	}

	resource, err := m.resourceForLaunch(ctx, accountID, userID, user, resourceID, scope)
	if err != nil {
		return err
	}
	if err := m.recordRecentVisit(ctx, accountID, userID, resource.ID, resource.Scope); err != nil {
		return err
	}
	if m.eventStore == nil {
		return nil
	}

	meta := map[string]any{
		"name":     resource.Name,
		"url":      resource.URL,
		"scope":    resource.Scope,
		"source":   resource.Source,
		"category": resource.Category,
	}
	m.eventStore.StoreEvent(ctx, userID, resource.ID, accountID, activity.WorkbenchResourceLaunched, meta)
	return nil
}

func (m *manager) ensureAdminAccess(ctx context.Context, accountID, userID string, operation operations.Operation) error {
	if m.permissionsManager != nil {
		allowed, _, err := m.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operation)
		if err != nil {
			return status.NewPermissionValidationError(err)
		}
		if !allowed {
			return status.NewPermissionDeniedError()
		}
		return nil
	}

	user, err := m.store.GetUserByUserID(ctx, store.LockingStrengthNone, userID)
	if err != nil {
		return err
	}
	if err := ensureUserInAccount(user, accountID); err != nil {
		return err
	}
	if !user.HasAdminPower() {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func (m *manager) EnsureAdminAssetAccess(ctx context.Context, accountID, userID string) error {
	if m.permissionsManager == nil {
		user, err := m.store.GetUserByUserID(ctx, store.LockingStrengthNone, userID)
		if err != nil {
			return err
		}
		if err := ensureUserInAccount(user, accountID); err != nil {
			return err
		}
		if !user.HasAdminPower() {
			return status.NewPermissionDeniedError()
		}
		return nil
	}

	allowed, _, err := m.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operations.Create)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if allowed {
		return nil
	}
	allowed, _, err = m.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Settings, operations.Update)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if allowed {
		return nil
	}
	return status.NewPermissionDeniedError()
}

func (m *manager) EnsurePersonalAssetAccess(ctx context.Context, accountID, userID string) error {
	_, err := m.getAccountUser(ctx, accountID, userID)
	return err
}

func (m *manager) getAccountUser(ctx context.Context, accountID, userID string) (*nbtypes.User, error) {
	user, err := m.store.GetUserByUserID(ctx, store.LockingStrengthNone, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureUserInAccount(user, accountID); err != nil {
		return nil, err
	}
	return user, nil
}

func (m *manager) validateResourceVisibility(ctx context.Context, accountID string, resource *types.Resource) error {
	if resource.Visibility != types.VisibilityRestricted {
		return nil
	}
	if len(resource.VisibleGroups) > 0 {
		groups, err := m.store.GetGroupsByIDs(ctx, store.LockingStrengthNone, accountID, resource.VisibleGroups)
		if err != nil {
			return err
		}
		for _, groupID := range resource.VisibleGroups {
			group, ok := groups[groupID]
			if !ok || group == nil {
				return status.Errorf(status.InvalidArgument, "visible group %s not found", groupID)
			}
			if group.Type != nbtypes.GroupTypeUser {
				return status.Errorf(status.InvalidArgument, "visible group %s must be a user group", groupID)
			}
		}
	}
	for _, visibleUserID := range resource.VisibleUsers {
		user, err := m.store.GetUserByUserID(ctx, store.LockingStrengthNone, visibleUserID)
		if err != nil {
			return status.Errorf(status.InvalidArgument, "visible user %s not found", visibleUserID)
		}
		if user == nil || user.AccountID != accountID {
			return status.Errorf(status.InvalidArgument, "visible user %s not found", visibleUserID)
		}
	}
	return nil
}

func ensureUserInAccount(user *nbtypes.User, accountID string) error {
	if user == nil || user.AccountID != accountID {
		return status.NewPermissionDeniedError()
	}
	return nil
}

func normalizeResourceID(resourceID string) (string, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return "", status.Errorf(status.InvalidArgument, "resource id shouldn't be empty")
	}
	return resourceID, nil
}

func normalizeAssetID(assetID string) (string, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return "", status.Errorf(status.InvalidArgument, "asset id shouldn't be empty")
	}
	return assetID, nil
}

func ensureResourcePayload(resource *types.Resource) error {
	if resource == nil {
		return status.Errorf(status.InvalidArgument, "resource payload shouldn't be empty")
	}
	return nil
}

func isVisibleToUser(resource *types.Resource, user *nbtypes.User) bool {
	if resource.Visibility == "" || resource.Visibility == types.VisibilityAll {
		return true
	}
	if slices.Contains(resource.VisibleUsers, user.Id) {
		return true
	}
	for _, groupID := range user.UserGroups {
		if slices.Contains(resource.VisibleGroups, groupID) {
			return true
		}
	}
	return false
}

func (m *manager) resourceForLaunch(ctx context.Context, accountID, userID string, user *nbtypes.User, resourceID, scope string) (*types.Resource, error) {
	switch scope {
	case types.ResourceScopeServer:
		resource, err := m.store.GetWorkbenchServerResource(ctx, accountID, resourceID)
		if err != nil {
			return nil, err
		}
		if resource == nil || !resource.Enabled {
			return nil, status.Errorf(status.NotFound, "workbench resource not found")
		}
		normalized := normalizeServerResourceForClient(accountID, *resource)
		if !isVisibleToUser(&normalized, user) {
			return nil, status.Errorf(status.NotFound, "workbench resource not found")
		}
		public := publicResourceForClient(normalized)
		return &public, nil
	case types.ResourceScopePersonal:
		resources, err := m.store.GetWorkbenchUserResources(ctx, accountID, userID)
		if err != nil {
			return nil, err
		}
		normalized := normalizePersonalResources(accountID, userID, resources)
		for i := range normalized {
			if normalized[i].ID == resourceID {
				public := publicResourceForClient(normalized[i])
				return &public, nil
			}
		}
		return nil, status.Errorf(status.NotFound, "workbench resource not found")
	default:
		return nil, status.Errorf(status.InvalidArgument, "resource scope is invalid")
	}
}

func normalizeServerResource(accountID, userID string, resource *types.Resource) {
	if resource.ID == "" {
		resource.ID = xid.New().String()
	}
	resource.AccountID = accountID
	resource.UserID = ""
	resource.Scope = types.ResourceScopeServer
	resource.Source = types.ResourceSourceAdmin
	normalizeResourceFields(resource)
	if resource.Visibility == "" {
		resource.Visibility = types.VisibilityAll
	}
	resource.Tags = uniqueTrimmedStrings(resource.Tags)
	if resource.Visibility == types.VisibilityRestricted {
		resource.VisibleGroups = uniqueTrimmedStrings(resource.VisibleGroups)
		resource.VisibleUsers = uniqueTrimmedStrings(resource.VisibleUsers)
	} else {
		resource.VisibleGroups = nil
		resource.VisibleUsers = nil
	}
	resource.CreatedBy = userID
}

func normalizePersonalResource(accountID, userID string, resource *types.Resource) {
	if resource.ID == "" {
		resource.ID = xid.New().String()
	}
	resource.AccountID = accountID
	resource.UserID = userID
	resource.Scope = types.ResourceScopePersonal
	resource.Source = types.ResourceSourceUser
	normalizeResourceFields(resource)
	resource.Visibility = ""
	resource.VisibleGroups = nil
	resource.VisibleUsers = nil
	resource.CreatedBy = ""
	resource.Tags = uniqueTrimmedStrings(resource.Tags)
	if resource.IconMode == "" {
		resource.IconMode = types.IconModeLetter
	}
}

func normalizeResourceFields(resource *types.Resource) {
	resource.Name = strings.TrimSpace(resource.Name)
	resource.Category = strings.TrimSpace(resource.Category)
	resource.Description = strings.TrimSpace(resource.Description)
	resource.IconURL = strings.TrimSpace(resource.IconURL)
	resource.IconMode = strings.TrimSpace(resource.IconMode)
	resource.URL = strings.TrimSpace(resource.URL)
}

func normalizePersonalResources(accountID, userID string, resources []types.Resource) []types.Resource {
	normalized := make([]types.Resource, 0, len(resources))
	for i := range resources {
		resource := resources[i]
		normalizePersonalResource(accountID, userID, &resource)
		if !resource.Enabled {
			continue
		}
		normalized = append(normalized, resource)
	}
	return normalized
}

func normalizeServerResourceForClient(accountID string, resource types.Resource) types.Resource {
	resource.AccountID = accountID
	resource.UserID = ""
	resource.Scope = types.ResourceScopeServer
	resource.Source = types.ResourceSourceAdmin
	normalizeResourceFields(&resource)
	resource.Tags = uniqueTrimmedStrings(resource.Tags)
	if resource.Visibility == "" {
		resource.Visibility = types.VisibilityAll
	}
	return resource
}

func normalizeServerResourceForAdmin(accountID string, resource types.Resource) types.Resource {
	resource = normalizeServerResourceForClient(accountID, resource)
	if resource.Visibility == types.VisibilityRestricted {
		resource.VisibleGroups = uniqueTrimmedStrings(resource.VisibleGroups)
		resource.VisibleUsers = uniqueTrimmedStrings(resource.VisibleUsers)
	} else {
		resource.VisibleGroups = nil
		resource.VisibleUsers = nil
	}
	return resource
}

func publicResourcesForClient(resources []types.Resource) []types.Resource {
	public := make([]types.Resource, 0, len(resources))
	for _, resource := range resources {
		public = append(public, publicResourceForClient(resource))
	}
	return public
}

func publicResourceForClient(resource types.Resource) types.Resource {
	resource.Visibility = ""
	resource.VisibleGroups = nil
	resource.VisibleUsers = nil
	resource.CreatedBy = ""
	return resource
}

func validateResource(resource *types.Resource) error {
	if strings.TrimSpace(resource.Name) == "" {
		return status.Errorf(status.InvalidArgument, "resource name shouldn't be empty")
	}
	if strings.TrimSpace(resource.URL) == "" {
		return status.Errorf(status.InvalidArgument, "resource url shouldn't be empty")
	}
	parsed, err := url.Parse(resource.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return status.Errorf(status.InvalidArgument, "resource url is invalid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return status.Errorf(status.InvalidArgument, "resource url must use http or https")
	}
	if resource.Visibility != "" && resource.Visibility != types.VisibilityAll && resource.Visibility != types.VisibilityRestricted {
		return status.Errorf(status.InvalidArgument, "resource visibility is invalid")
	}
	if resource.IconMode != "" && !isValidIconMode(resource.IconMode) {
		return status.Errorf(status.InvalidArgument, "resource icon mode is invalid")
	}
	return nil
}

func isValidIconMode(iconMode string) bool {
	return iconMode == types.IconModeUploaded ||
		iconMode == types.IconModeFetched ||
		iconMode == types.IconModeLetter
}

func defaultCategories(server []types.Resource, personal []types.Resource) []types.Category {
	categories := []types.Category{
		{ID: "all", Name: "全部资源", Sort: 0},
		{ID: "server", Name: "服务器资源", Sort: 5},
		{ID: "personal", Name: "个人资源", Sort: 6},
	}
	seen := map[string]struct{}{
		"全部资源":  {},
		"服务器资源": {},
		"个人资源":  {},
	}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		categories = append(categories, types.Category{ID: fmt.Sprintf("category-%d", len(categories)), Name: name, Sort: len(categories) * 10})
	}
	for _, resource := range server {
		add(resource.Category)
	}
	for _, resource := range personal {
		add(resource.Category)
	}
	return categories
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (m *manager) applyRecentVisits(ctx context.Context, accountID, userID string, serverResources, personalResources []types.Resource) error {
	state, err := m.store.GetWorkbenchUserState(ctx, accountID, userID)
	if err != nil {
		return err
	}
	recentByKey := make(map[string]types.RecentVisit, len(state.RecentVisits))
	for _, visit := range state.RecentVisits {
		if visit.ResourceID == "" || visit.Scope == "" || visit.VisitedAt.IsZero() {
			continue
		}
		recentByKey[visit.Scope+":"+visit.ResourceID] = visit
	}
	for i := range serverResources {
		if visit, ok := recentByKey[serverResources[i].Scope+":"+serverResources[i].ID]; ok {
			if serverResources[i].Metadata == nil {
				serverResources[i].Metadata = map[string]any{}
			}
			serverResources[i].Metadata["recentVisitedAt"] = visit.VisitedAt.UTC().Format(time.RFC3339)
		}
	}
	for i := range personalResources {
		if visit, ok := recentByKey[personalResources[i].Scope+":"+personalResources[i].ID]; ok {
			if personalResources[i].Metadata == nil {
				personalResources[i].Metadata = map[string]any{}
			}
			personalResources[i].Metadata["recentVisitedAt"] = visit.VisitedAt.UTC().Format(time.RFC3339)
		}
	}
	return nil
}

func (m *manager) recordRecentVisit(ctx context.Context, accountID, userID, resourceID, scope string) error {
	state, err := m.store.GetWorkbenchUserState(ctx, accountID, userID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	recentVisits := make([]types.RecentVisit, 0, len(state.RecentVisits)+1)
	recentVisits = append(recentVisits, types.RecentVisit{
		ResourceID: resourceID,
		Scope:      scope,
		VisitedAt:  now,
	})
	for _, visit := range state.RecentVisits {
		if visit.ResourceID == resourceID && visit.Scope == scope {
			continue
		}
		if visit.ResourceID == "" || visit.Scope == "" || visit.VisitedAt.IsZero() {
			continue
		}
		recentVisits = append(recentVisits, visit)
		if len(recentVisits) >= 20 {
			break
		}
	}
	return m.store.SaveWorkbenchUserRecentVisits(ctx, accountID, userID, recentVisits)
}

func sortResources(resources []types.Resource) {
	slices.SortFunc(resources, func(left, right types.Resource) int {
		if left.Favorite != right.Favorite {
			if left.Favorite {
				return -1
			}
			return 1
		}
		if left.Sort != right.Sort {
			return left.Sort - right.Sort
		}
		if cmp := strings.Compare(left.Name, right.Name); cmp != 0 {
			return cmp
		}
		return strings.Compare(left.ID, right.ID)
	})
}

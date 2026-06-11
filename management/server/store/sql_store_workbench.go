package store

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workbenchTypes "github.com/netbirdio/netbird/management/server/workbench/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func (s *SqlStore) GetWorkbenchServerResources(ctx context.Context, accountID string) ([]*workbenchTypes.Resource, error) {
	var resources []*workbenchTypes.Resource
	result := s.db.
		Where("account_id = ? AND scope = ?", accountID, workbenchTypes.ResourceScopeServer).
		Order("sort ASC, name ASC").
		Find(&resources)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to get workbench server resources: %v", result.Error)
		return nil, status.Errorf(status.Internal, "failed to get workbench server resources")
	}

	if err := s.populateWorkbenchVisibility(ctx, accountID, resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func (s *SqlStore) GetWorkbenchServerResource(ctx context.Context, accountID, resourceID string) (*workbenchTypes.Resource, error) {
	var resource workbenchTypes.Resource
	result := s.db.
		Where("account_id = ? AND scope = ?", accountID, workbenchTypes.ResourceScopeServer).
		Take(&resource, idQueryCondition, resourceID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(status.NotFound, "workbench resource not found")
		}
		log.WithContext(ctx).Errorf("failed to get workbench server resource: %v", result.Error)
		return nil, status.Errorf(status.Internal, "failed to get workbench server resource")
	}
	resources := []*workbenchTypes.Resource{&resource}
	if err := s.populateWorkbenchVisibility(ctx, accountID, resources); err != nil {
		return nil, err
	}
	return &resource, nil
}

func (s *SqlStore) SaveWorkbenchServerResource(ctx context.Context, resource *workbenchTypes.Resource) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(resource).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to save workbench server resource: %v", err)
			return status.Errorf(status.Internal, "failed to save workbench server resource")
		}
		if err := tx.Delete(&workbenchTypes.ResourceVisibleGroup{}, "account_id = ? AND resource_id = ?", resource.AccountID, resource.ID).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to clear workbench resource visible groups: %v", err)
			return status.Errorf(status.Internal, "failed to save workbench resource visible groups")
		}
		if err := tx.Delete(&workbenchTypes.ResourceVisibleUser{}, "account_id = ? AND resource_id = ?", resource.AccountID, resource.ID).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to clear workbench resource visible users: %v", err)
			return status.Errorf(status.Internal, "failed to save workbench resource visible users")
		}
		if len(resource.VisibleGroups) > 0 {
			groups := make([]workbenchTypes.ResourceVisibleGroup, 0, len(resource.VisibleGroups))
			for _, groupID := range resource.VisibleGroups {
				groups = append(groups, workbenchTypes.ResourceVisibleGroup{
					AccountID:  resource.AccountID,
					ResourceID: resource.ID,
					GroupID:    groupID,
				})
			}
			if err := tx.Create(&groups).Error; err != nil {
				log.WithContext(ctx).Errorf("failed to save workbench resource visible groups: %v", err)
				return status.Errorf(status.Internal, "failed to save workbench resource visible groups")
			}
		}
		if len(resource.VisibleUsers) > 0 {
			users := make([]workbenchTypes.ResourceVisibleUser, 0, len(resource.VisibleUsers))
			for _, userID := range resource.VisibleUsers {
				users = append(users, workbenchTypes.ResourceVisibleUser{
					AccountID:  resource.AccountID,
					ResourceID: resource.ID,
					UserID:     userID,
				})
			}
			if err := tx.Create(&users).Error; err != nil {
				log.WithContext(ctx).Errorf("failed to save workbench resource visible users: %v", err)
				return status.Errorf(status.Internal, "failed to save workbench resource visible users")
			}
		}
		return nil
	})
}

func (s *SqlStore) DeleteWorkbenchServerResource(ctx context.Context, accountID, resourceID string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&workbenchTypes.ResourceVisibleGroup{}, "account_id = ? AND resource_id = ?", accountID, resourceID).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to delete workbench visible groups: %v", err)
			return status.Errorf(status.Internal, "failed to delete workbench resource")
		}
		if err := tx.Delete(&workbenchTypes.ResourceVisibleUser{}, "account_id = ? AND resource_id = ?", accountID, resourceID).Error; err != nil {
			log.WithContext(ctx).Errorf("failed to delete workbench visible users: %v", err)
			return status.Errorf(status.Internal, "failed to delete workbench resource")
		}
		result := tx.Delete(&workbenchTypes.Resource{}, "account_id = ? AND id = ? AND scope = ?", accountID, resourceID, workbenchTypes.ResourceScopeServer)
		if result.Error != nil {
			log.WithContext(ctx).Errorf("failed to delete workbench server resource: %v", result.Error)
			return status.Errorf(status.Internal, "failed to delete workbench resource")
		}
		if result.RowsAffected == 0 {
			return status.Errorf(status.NotFound, "workbench resource not found")
		}
		return nil
	})
}

func (s *SqlStore) GetWorkbenchUserResources(ctx context.Context, accountID, userID string) ([]workbenchTypes.Resource, error) {
	var userResources workbenchTypes.UserResources
	result := s.db.Take(&userResources, "account_id = ? AND user_id = ?", accountID, userID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return []workbenchTypes.Resource{}, nil
		}
		log.WithContext(ctx).Errorf("failed to get workbench user resources: %v", result.Error)
		return nil, status.Errorf(status.Internal, "failed to get workbench user resources")
	}
	return userResources.ResourcesJSON, nil
}

func (s *SqlStore) GetWorkbenchUserState(ctx context.Context, accountID, userID string) (*workbenchTypes.UserResources, error) {
	var userResources workbenchTypes.UserResources
	result := s.db.Take(&userResources, "account_id = ? AND user_id = ?", accountID, userID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return &workbenchTypes.UserResources{
				AccountID:     accountID,
				UserID:        userID,
				ResourcesJSON: []workbenchTypes.Resource{},
				RecentVisits:  []workbenchTypes.RecentVisit{},
			}, nil
		}
		log.WithContext(ctx).Errorf("failed to get workbench user state: %v", result.Error)
		return nil, status.Errorf(status.Internal, "failed to get workbench user resources")
	}
	return &userResources, nil
}

func (s *SqlStore) SaveWorkbenchUserResources(ctx context.Context, accountID, userID string, resources []workbenchTypes.Resource) error {
	state, err := s.GetWorkbenchUserState(ctx, accountID, userID)
	if err != nil {
		return err
	}
	userResources := &workbenchTypes.UserResources{
		AccountID:     accountID,
		UserID:        userID,
		ResourcesJSON: resources,
		RecentVisits:  state.RecentVisits,
	}
	result := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"resources_json", "recent_visits", "updated_at"}),
	}).Create(userResources)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to save workbench user resources: %v", result.Error)
		return status.Errorf(status.Internal, "failed to save workbench user resources")
	}
	return nil
}

func (s *SqlStore) SaveWorkbenchUserRecentVisits(ctx context.Context, accountID, userID string, recentVisits []workbenchTypes.RecentVisit) error {
	state, err := s.GetWorkbenchUserState(ctx, accountID, userID)
	if err != nil {
		return err
	}
	userResources := &workbenchTypes.UserResources{
		AccountID:     accountID,
		UserID:        userID,
		ResourcesJSON: state.ResourcesJSON,
		RecentVisits:  recentVisits,
	}
	result := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"resources_json", "recent_visits", "updated_at"}),
	}).Create(userResources)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to save workbench user recent visits: %v", result.Error)
		return status.Errorf(status.Internal, "failed to save workbench user resources")
	}
	return nil
}

func (s *SqlStore) SaveWorkbenchAsset(ctx context.Context, asset *workbenchTypes.Asset) error {
	result := s.db.Save(asset)
	if result.Error != nil {
		log.WithContext(ctx).Errorf("failed to save workbench asset: %v", result.Error)
		return status.Errorf(status.Internal, "failed to save workbench asset")
	}
	return nil
}

func (s *SqlStore) GetWorkbenchAsset(ctx context.Context, accountID, assetID string) (*workbenchTypes.Asset, error) {
	var asset workbenchTypes.Asset
	result := s.db.Take(&asset, "account_id = ? AND id = ?", accountID, assetID)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(status.NotFound, "workbench asset not found")
		}
		log.WithContext(ctx).Errorf("failed to get workbench asset: %v", result.Error)
		return nil, status.Errorf(status.Internal, "failed to get workbench asset")
	}
	return &asset, nil
}

func (s *SqlStore) populateWorkbenchVisibility(ctx context.Context, accountID string, resources []*workbenchTypes.Resource) error {
	if len(resources) == 0 {
		return nil
	}
	ids := make([]string, 0, len(resources))
	byID := make(map[string]*workbenchTypes.Resource, len(resources))
	for _, resource := range resources {
		ids = append(ids, resource.ID)
		byID[resource.ID] = resource
	}

	var groups []workbenchTypes.ResourceVisibleGroup
	if err := s.db.Where("account_id = ? AND resource_id IN ?", accountID, ids).Order("resource_id ASC, group_id ASC").Find(&groups).Error; err != nil {
		log.WithContext(ctx).Errorf("failed to get workbench visible groups: %v", err)
		return status.Errorf(status.Internal, "failed to get workbench resource visibility")
	}
	for _, group := range groups {
		if resource := byID[group.ResourceID]; resource != nil {
			resource.VisibleGroups = append(resource.VisibleGroups, group.GroupID)
		}
	}

	var users []workbenchTypes.ResourceVisibleUser
	if err := s.db.Where("account_id = ? AND resource_id IN ?", accountID, ids).Order("resource_id ASC, user_id ASC").Find(&users).Error; err != nil {
		log.WithContext(ctx).Errorf("failed to get workbench visible users: %v", err)
		return status.Errorf(status.Internal, "failed to get workbench resource visibility")
	}
	for _, user := range users {
		if resource := byID[user.ResourceID]; resource != nil {
			resource.VisibleUsers = append(resource.VisibleUsers, user.UserID)
		}
	}
	return nil
}

package server

import (
	"context"
	_ "embed"

	"github.com/rs/xid"
	"github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/entitlements"
	"github.com/netbirdio/netbird/management/server/posture"
	"github.com/netbirdio/netbird/shared/management/status"
)

// GetPolicy from the store
func (am *DefaultAccountManager) GetPolicy(ctx context.Context, accountID, policyID, userID string) (*types.Policy, error) {
	allowed, ctx, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Policies, operations.Read)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	return am.Store.GetPolicyByID(ctx, store.LockingStrengthNone, accountID, policyID)
}

// SavePolicy in the store
func (am *DefaultAccountManager) SavePolicy(ctx context.Context, accountID, userID string, policy *types.Policy, create bool) (*types.Policy, error) {
	operation := operations.Create
	if !create {
		operation = operations.Update
	}
	allowed, ctx, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Policies, operation)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	if len(policy.SourcePostureChecks) > 0 {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureDevicePosture); err != nil {
			return nil, err
		}
	}
	if policyUsesNetbirdSSH(policy) {
		if err := am.requireEntitledFeature(ctx, accountID, entitlements.FeatureWebSSH); err != nil {
			return nil, err
		}
	}

	var isUpdate = policy.ID != ""
	var updateAccountPeers bool
	var action = activity.PolicyAdded
	var unchanged bool

	err = am.Store.ExecuteInTransaction(ctx, func(transaction store.Store) error {
		existingPolicy, err := validatePolicy(ctx, transaction, accountID, policy)
		if err != nil {
			return err
		}

		if isUpdate {
			if policy.Equal(existingPolicy) {
				logrus.WithContext(ctx).Tracef("policy update skipped because equal to stored one - policy id %s", policy.ID)
				unchanged = true
				return nil
			}

			action = activity.PolicyUpdated

			updateAccountPeers, err = arePolicyChangesAffectPeersWithExisting(ctx, transaction, policy, existingPolicy)
			if err != nil {
				return err
			}

			if err = transaction.SavePolicy(ctx, policy); err != nil {
				return err
			}
		} else {
			updateAccountPeers, err = arePolicyChangesAffectPeers(ctx, transaction, policy)
			if err != nil {
				return err
			}

			if err = transaction.CreatePolicy(ctx, policy); err != nil {
				return err
			}
		}

		return transaction.IncrementNetworkSerial(ctx, accountID)
	})
	if err != nil {
		return nil, err
	}

	if unchanged {
		return policy, nil
	}

	am.StoreEvent(ctx, userID, policy.ID, accountID, action, policy.EventMeta())

	if updateAccountPeers {
		policyOp := types.UpdateOperationCreate
		if isUpdate {
			policyOp = types.UpdateOperationUpdate
		}
		am.UpdateAccountPeers(ctx, accountID, types.UpdateReason{Resource: types.UpdateResourcePolicy, Operation: policyOp})
	}

	return policy, nil
}

// DeletePolicy from the store
func (am *DefaultAccountManager) DeletePolicy(ctx context.Context, accountID, policyID, userID string) error {
	allowed, ctx, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Policies, operations.Delete)
	if err != nil {
		return status.NewPermissionValidationError(err)
	}
	if !allowed {
		return status.NewPermissionDeniedError()
	}

	var policy *types.Policy
	var updateAccountPeers bool

	err = am.Store.ExecuteInTransaction(ctx, func(transaction store.Store) error {
		policy, err = transaction.GetPolicyByID(ctx, store.LockingStrengthUpdate, accountID, policyID)
		if err != nil {
			return err
		}

		updateAccountPeers, err = arePolicyChangesAffectPeers(ctx, transaction, policy)
		if err != nil {
			return err
		}

		if err = transaction.DeletePolicy(ctx, accountID, policyID); err != nil {
			return err
		}

		return transaction.IncrementNetworkSerial(ctx, accountID)
	})
	if err != nil {
		return err
	}

	am.StoreEvent(ctx, userID, policyID, accountID, activity.PolicyRemoved, policy.EventMeta())

	if updateAccountPeers {
		am.UpdateAccountPeers(ctx, accountID, types.UpdateReason{Resource: types.UpdateResourcePolicy, Operation: types.UpdateOperationDelete})
	}

	return nil
}

// ListPolicies from the store.
func (am *DefaultAccountManager) ListPolicies(ctx context.Context, accountID, userID string) ([]*types.Policy, error) {
	allowed, ctx, err := am.permissionsManager.ValidateUserPermissions(ctx, accountID, userID, modules.Policies, operations.Read)
	if err != nil {
		return nil, status.NewPermissionValidationError(err)
	}
	if !allowed {
		return nil, status.NewPermissionDeniedError()
	}

	return am.Store.GetAccountPolicies(ctx, store.LockingStrengthNone, accountID)
}

// arePolicyChangesAffectPeers checks if a policy (being created or deleted) will affect any associated peers.
func arePolicyChangesAffectPeers(ctx context.Context, transaction store.Store, policy *types.Policy) (bool, error) {
	for _, rule := range policy.Rules {
		if rule.SourceResource.Type != "" || rule.DestinationResource.Type != "" || len(rule.SourceUsers) != 0 || len(rule.SourceUserGroups) != 0 {
			return true, nil
		}
	}

	return anyGroupHasPeersOrResources(ctx, transaction, policy.AccountID, policy.RuleGroups())
}

func arePolicyChangesAffectPeersWithExisting(ctx context.Context, transaction store.Store, policy *types.Policy, existingPolicy *types.Policy) (bool, error) {
	if !policy.Enabled && !existingPolicy.Enabled {
		return false, nil
	}

	for _, rule := range existingPolicy.Rules {
		if rule.SourceResource.Type != "" || rule.DestinationResource.Type != "" || len(rule.SourceUsers) != 0 || len(rule.SourceUserGroups) != 0 {
			return true, nil
		}
	}

	hasPeers, err := anyGroupHasPeersOrResources(ctx, transaction, policy.AccountID, existingPolicy.RuleGroups())
	if err != nil {
		return false, err
	}

	if hasPeers {
		return true, nil
	}

	for _, rule := range policy.Rules {
		if rule.SourceResource.Type != "" || rule.DestinationResource.Type != "" || len(rule.SourceUsers) != 0 || len(rule.SourceUserGroups) != 0 {
			return true, nil
		}
	}

	return anyGroupHasPeersOrResources(ctx, transaction, policy.AccountID, policy.RuleGroups())
}

// validatePolicy validates the policy and its rules. For updates it returns
// the existing policy loaded from the store so callers can avoid a second read.
func validatePolicy(ctx context.Context, transaction store.Store, accountID string, policy *types.Policy) (*types.Policy, error) {
	var existingPolicy *types.Policy
	if policy.ID != "" {
		var err error
		existingPolicy, err = transaction.GetPolicyByID(ctx, store.LockingStrengthNone, accountID, policy.ID)
		if err != nil {
			return nil, err
		}

		existingRuleIDs := make(map[string]bool)
		for _, rule := range existingPolicy.Rules {
			existingRuleIDs[rule.ID] = true
		}

		for _, rule := range policy.Rules {
			if rule.ID != "" && !existingRuleIDs[rule.ID] {
				return nil, status.Errorf(status.InvalidArgument, "invalid rule ID: %s", rule.ID)
			}
		}
	} else {
		policy.ID = xid.New().String()
		policy.AccountID = accountID
	}

	groups, err := transaction.GetGroupsByIDs(ctx, store.LockingStrengthNone, accountID, allPolicyGroupIDs(policy))
	if err != nil {
		return nil, err
	}
	users, err := transaction.GetAccountUsers(ctx, store.LockingStrengthNone, accountID)
	if err != nil {
		return nil, err
	}
	usersByID := make(map[string]*types.User, len(users))
	for _, user := range users {
		usersByID[user.Id] = user
	}

	postureChecks, err := transaction.GetPostureChecksByIDs(ctx, store.LockingStrengthNone, accountID, policy.SourcePostureChecks)
	if err != nil {
		return nil, err
	}

	for i, rule := range policy.Rules {
		ruleCopy := rule.Copy()
		if ruleCopy.ID == "" {
			ruleCopy.ID = xid.New().String()
			ruleCopy.PolicyID = policy.ID
		}

		ruleCopy.Sources = getValidGroupIDsByType(groups, ruleCopy.Sources, types.GroupTypePeer)
		ruleCopy.SourceUsers = getValidUserIDs(usersByID, ruleCopy.SourceUsers)
		ruleCopy.SourceUserGroups = getValidGroupIDsByType(groups, ruleCopy.SourceUserGroups, types.GroupTypeUser)
		ruleCopy.Destinations = getValidGroupIDsByType(groups, ruleCopy.Destinations, types.GroupTypePeer)
		policy.Rules[i] = ruleCopy
	}

	if policy.SourcePostureChecks != nil {
		policy.SourcePostureChecks = getValidPostureCheckIDs(postureChecks, policy.SourcePostureChecks)
	}

	return existingPolicy, nil
}

func allPolicyGroupIDs(policy *types.Policy) []string {
	ids := policy.RuleGroups()
	for _, groupID := range policy.SourceUserGroups() {
		ids = append(ids, groupID)
	}
	return ids
}

// getValidPostureCheckIDs filters and returns only the valid posture check IDs from the provided list.
func getValidPostureCheckIDs(postureChecks map[string]*posture.Checks, postureChecksIds []string) []string {
	validIDs := make([]string, 0, len(postureChecksIds))
	for _, id := range postureChecksIds {
		if _, exists := postureChecks[id]; exists {
			validIDs = append(validIDs, id)
		}
	}

	return validIDs
}

func policyUsesNetbirdSSH(policy *types.Policy) bool {
	if policy == nil {
		return false
	}
	for _, rule := range policy.Rules {
		if rule != nil && rule.Protocol == types.PolicyRuleProtocolNetbirdSSH {
			return true
		}
	}
	return false
}

// getValidGroupIDsByType filters and returns only valid group IDs of the requested usage type.
func getValidGroupIDsByType(groups map[string]*types.Group, groupIDs []string, groupType string) []string {
	validIDs := make([]string, 0, len(groupIDs))
	for _, id := range groupIDs {
		if group, exists := groups[id]; exists && (group.Type == groupType || group.Type == "") {
			validIDs = append(validIDs, id)
		}
	}

	return validIDs
}

// getValidUserIDs filters and returns only the valid user IDs from the provided list.
func getValidUserIDs(users map[string]*types.User, userIDs []string) []string {
	validIDs := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if user, exists := users[id]; exists && !user.IsServiceUser {
			validIDs = append(validIDs, id)
		}
	}

	return validIDs
}

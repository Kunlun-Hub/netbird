package workbench

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/netbirdio/netbird/management/server/activity"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	nbtypes "github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/management/server/workbench/types"
	"github.com/netbirdio/netbird/shared/management/status"
)

func TestAdminResourcesUseSettingsPermissions(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	resource := &types.Resource{
		ID:         "resource-1",
		AccountID:  accountID,
		Scope:      types.ResourceScopeServer,
		Name:       "Portal",
		URL:        "https://example.com",
		Enabled:    true,
		Visibility: types.VisibilityAll,
	}

	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleUser},
		resources: []*types.Resource{resource},
	}
	permissions := &fakePermissionsManager{allowed: true}
	manager := NewManager(store, permissions)

	resources, err := manager.ListAdminResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListAdminResources() error = %v", err)
	}
	if len(resources) != 1 || resources[0].ID != resource.ID {
		t.Fatalf("ListAdminResources() resources = %+v, want resource %s", resources, resource.ID)
	}
	if permissions.module != modules.Settings || permissions.operation != operations.Read {
		t.Fatalf("ValidateUserPermissions() module=%s operation=%s, want settings/read", permissions.module, permissions.operation)
	}

	permissions.allowed = false
	if _, err := manager.ListAdminResources(ctx, accountID, userID); err == nil {
		t.Fatal("ListAdminResources() error = nil, want permission denied")
	}
}

func TestAdminResourcesAreNormalizedForDashboard(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	stored := &types.Resource{
		ID:            "resource-1",
		AccountID:     "stale-account",
		UserID:        "stale-user",
		Scope:         "",
		Source:        "",
		Name:          " Portal ",
		Category:      " 企业资源 ",
		Description:   " 描述 ",
		IconURL:       " /api/workbench/assets/icon-1 ",
		IconMode:      " letter ",
		URL:           " https://portal.example.com ",
		Tags:          []string{" 门户 ", "", "门户", "SSO"},
		Enabled:       true,
		Visibility:    "",
		VisibleGroups: []string{" group-1 "},
		VisibleUsers:  []string{" user-1 "},
		CreatedBy:     "creator-1",
	}
	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		resources: []*types.Resource{stored},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	resources, err := manager.ListAdminResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListAdminResources() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("ListAdminResources() length = %d, want 1", len(resources))
	}
	got := resources[0]
	if got.AccountID != accountID ||
		got.UserID != "" ||
		got.Scope != types.ResourceScopeServer ||
		got.Source != types.ResourceSourceAdmin ||
		got.Name != "Portal" ||
		got.Category != "企业资源" ||
		got.Description != "描述" ||
		got.IconURL != "/api/workbench/assets/icon-1" ||
		got.IconMode != types.IconModeLetter ||
		got.URL != "https://portal.example.com" ||
		got.Visibility != types.VisibilityAll ||
		len(got.VisibleGroups) != 0 ||
		len(got.VisibleUsers) != 0 ||
		!slices.Equal(got.Tags, []string{"门户", "SSO"}) {
		t.Fatalf("normalized admin resource mismatch: %+v", got)
	}

	detail, err := manager.GetAdminResource(ctx, accountID, userID, "resource-1")
	if err != nil {
		t.Fatalf("GetAdminResource() error = %v", err)
	}
	if detail == nil || detail.Name != "Portal" || detail.Visibility != types.VisibilityAll || len(detail.VisibleGroups) != 0 {
		t.Fatalf("normalized admin detail mismatch: %+v", detail)
	}
	if stored.Name != " Portal " || stored.AccountID != "stale-account" || len(stored.VisibleGroups) != 1 || stored.VisibleGroups[0] != " group-1 " {
		t.Fatalf("stored resource was mutated: %+v", stored)
	}
}

func TestGetAdminResourceTreatsNilStoreResultAsNotFound(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user:                   &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		forceNilServerResource: true,
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.GetAdminResource(ctx, accountID, userID, "resource-1"); err == nil {
		t.Fatal("GetAdminResource() error = nil, want not found")
	}
	if store.getServerResourceCalls != 1 {
		t.Fatalf("GetWorkbenchServerResource() calls = %d, want 1", store.getServerResourceCalls)
	}
}

func TestListResourcesHidesVisibilityConfiguration(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	resource := &types.Resource{
		ID:            "resource-1",
		AccountID:     accountID,
		Scope:         types.ResourceScopeServer,
		Name:          "Portal",
		URL:           "https://example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"group-1"},
		VisibleUsers:  []string{userID},
		CreatedBy:     "admin-1",
	}
	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleUser},
		resources: []*types.Resource{resource},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.ServerResources) != 1 {
		t.Fatalf("ListResources() server resources length = %d, want 1", len(resources.ServerResources))
	}
	got := resources.ServerResources[0]
	if got.Visibility != "" || len(got.VisibleGroups) != 0 || len(got.VisibleUsers) != 0 || got.CreatedBy != "" {
		t.Fatalf("ListResources() leaked visibility fields: %+v", got)
	}
	if resource.Visibility == "" || len(resource.VisibleGroups) == 0 || len(resource.VisibleUsers) == 0 || resource.CreatedBy == "" {
		t.Fatalf("ListResources() mutated stored resource: %+v", resource)
	}
}

func TestListResourcesTreatsEmptyServerVisibilityAsAll(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	resource := &types.Resource{
		ID:         "resource-legacy",
		AccountID:  accountID,
		Scope:      types.ResourceScopeServer,
		Name:       "Legacy Portal",
		URL:        "https://legacy.example.com",
		Enabled:    true,
		Visibility: "",
	}
	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleUser},
		resources: []*types.Resource{resource},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.ServerResources) != 1 {
		t.Fatalf("ListResources() server resources length = %d, want 1", len(resources.ServerResources))
	}
	if got, want := resources.ServerResources[0].ID, "resource-legacy"; got != want {
		t.Fatalf("visible resource ID = %q, want %q", got, want)
	}
}

func TestListResourcesSkipsNilServerResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	resource := &types.Resource{
		ID:         "resource-1",
		AccountID:  accountID,
		Scope:      types.ResourceScopeServer,
		Name:       "Portal",
		URL:        "https://portal.example.com",
		Enabled:    true,
		Visibility: types.VisibilityAll,
	}
	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleUser},
		resources: []*types.Resource{nil, resource},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if got, want := len(resources.ServerResources), 1; got != want {
		t.Fatalf("server resources length = %d, want %d: %+v", got, want, resources.ServerResources)
	}
	if got, want := resources.ServerResources[0].ID, "resource-1"; got != want {
		t.Fatalf("server resource ID = %q, want %q", got, want)
	}
}

func TestListResourcesRejectsUserFromDifferentAccount(t *testing.T) {
	ctx := context.Background()
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: "user-1", AccountID: "other-account", Role: nbtypes.UserRoleUser},
		resources: []*types.Resource{{
			ID:         "resource-1",
			AccountID:  "account-1",
			Scope:      types.ResourceScopeServer,
			Name:       "Portal",
			URL:        "https://example.com",
			Enabled:    true,
			Visibility: types.VisibilityAll,
		}},
	}
	manager := NewManager(store, nil)

	if _, err := manager.ListResources(ctx, "account-1", "user-1"); err == nil {
		t.Fatal("ListResources() error = nil, want permission denied for user from another account")
	}
}

func TestRecordLaunchStoresEventForVisibleServerResource(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	resource := &types.Resource{
		ID:         "server-1",
		AccountID:  accountID,
		Scope:      types.ResourceScopeServer,
		Source:     types.ResourceSourceAdmin,
		Name:       "Portal",
		Category:   "常用应用",
		URL:        "https://portal.example.com",
		Enabled:    true,
		Visibility: types.VisibilityAll,
	}
	eventStore := &fakeWorkbenchEventStore{}
	manager := NewManagerWithEvents(&workbenchMemoryStore{
		user:      &nbtypes.User{Id: userID, AccountID: accountID},
		resources: []*types.Resource{resource},
	}, nil, eventStore)

	if err := manager.RecordLaunch(ctx, accountID, userID, "server-1", types.ResourceScopeServer); err != nil {
		t.Fatalf("RecordLaunch() error = %v", err)
	}
	if eventStore.activity != activity.WorkbenchResourceLaunched {
		t.Fatalf("activity = %v, want %v", eventStore.activity, activity.WorkbenchResourceLaunched)
	}
	if eventStore.initiatorID != userID || eventStore.targetID != "server-1" || eventStore.accountID != accountID {
		t.Fatalf("event target/initiator/account = %q/%q/%q", eventStore.targetID, eventStore.initiatorID, eventStore.accountID)
	}
	if got, want := eventStore.meta["scope"], types.ResourceScopeServer; got != want {
		t.Fatalf("meta scope = %v, want %v", got, want)
	}
	if got, want := eventStore.meta["url"], "https://portal.example.com"; got != want {
		t.Fatalf("meta url = %v, want %v", got, want)
	}
}

func TestRecordLaunchRejectsInvisibleServerResource(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	manager := NewManagerWithEvents(&workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		resources: []*types.Resource{{
			ID:            "server-1",
			AccountID:     accountID,
			Scope:         types.ResourceScopeServer,
			Name:          "Portal",
			URL:           "https://portal.example.com",
			Enabled:       true,
			Visibility:    types.VisibilityRestricted,
			VisibleUsers:  []string{"user-2"},
			VisibleGroups: []string{"group-2"},
		}},
	}, nil, &fakeWorkbenchEventStore{})

	if err := manager.RecordLaunch(ctx, accountID, userID, "server-1", types.ResourceScopeServer); err == nil {
		t.Fatal("RecordLaunch() error = nil, want not found for invisible resource")
	}
}

func TestRecordLaunchStoresEventForPersonalResource(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	eventStore := &fakeWorkbenchEventStore{}
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{{
			ID:        "personal-1",
			AccountID: accountID,
			UserID:    userID,
			Scope:     types.ResourceScopePersonal,
			Source:    types.ResourceSourceUser,
			Name:      "Docs",
			URL:       "https://docs.example.com",
			Enabled:   true,
		}},
	}
	manager := NewManagerWithEvents(store, nil, eventStore)

	if err := manager.RecordLaunch(ctx, accountID, userID, "personal-1", types.ResourceScopePersonal); err != nil {
		t.Fatalf("RecordLaunch() error = %v", err)
	}
	if got, want := eventStore.meta["scope"], types.ResourceScopePersonal; got != want {
		t.Fatalf("meta scope = %v, want %v", got, want)
	}
	if got, want := eventStore.targetID, "personal-1"; got != want {
		t.Fatalf("targetID = %q, want %q", got, want)
	}
	if len(store.savedRecentVisits) != 1 ||
		store.savedRecentVisits[0].ResourceID != "personal-1" ||
		store.savedRecentVisits[0].Scope != types.ResourceScopePersonal ||
		store.savedRecentVisits[0].VisitedAt.IsZero() {
		t.Fatalf("saved recent visits = %+v, want personal resource launch", store.savedRecentVisits)
	}
}

func TestListResourcesMarksRecentVisibleResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	visitedAt := time.Date(2026, 6, 11, 10, 30, 0, 0, time.UTC)
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		resources: []*types.Resource{
			{
				ID:         "server-1",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Source:     types.ResourceSourceAdmin,
				Name:       "Portal",
				URL:        "https://portal.example.com",
				Enabled:    true,
				Visibility: types.VisibilityAll,
			},
			{
				ID:         "server-hidden",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Source:     types.ResourceSourceAdmin,
				Name:       "Hidden",
				URL:        "https://hidden.example.com",
				Enabled:    false,
				Visibility: types.VisibilityAll,
			},
		},
		userResources: []types.Resource{{
			ID:        "personal-1",
			AccountID: accountID,
			UserID:    userID,
			Scope:     types.ResourceScopePersonal,
			Source:    types.ResourceSourceUser,
			Name:      "Docs",
			URL:       "https://docs.example.com",
			Enabled:   true,
		}},
		recentVisits: []types.RecentVisit{
			{ResourceID: "server-1", Scope: types.ResourceScopeServer, VisitedAt: visitedAt},
			{ResourceID: "personal-1", Scope: types.ResourceScopePersonal, VisitedAt: visitedAt.Add(-time.Minute)},
			{ResourceID: "server-hidden", Scope: types.ResourceScopeServer, VisitedAt: visitedAt.Add(-2 * time.Minute)},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.ServerResources) != 1 || len(resources.PersonalResources) != 1 {
		t.Fatalf("resources = server %+v personal %+v, want one visible server and one personal", resources.ServerResources, resources.PersonalResources)
	}
	if got, want := resources.ServerResources[0].Metadata["recentVisitedAt"], visitedAt.Format(time.RFC3339); got != want {
		t.Fatalf("server recentVisitedAt = %v, want %v", got, want)
	}
	if got, want := resources.PersonalResources[0].Metadata["recentVisitedAt"], visitedAt.Add(-time.Minute).Format(time.RFC3339); got != want {
		t.Fatalf("personal recentVisitedAt = %v, want %v", got, want)
	}
}

func TestManagerRejectsNilUserFromStore(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{}
	manager := NewManager(store, nil)

	if _, err := manager.ListResources(ctx, accountID, userID); err == nil {
		t.Fatal("ListResources() error = nil, want permission denied for nil user")
	}
	if _, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{Name: "Personal", URL: "https://personal.example.com"}); err == nil {
		t.Fatal("CreatePersonalResource() error = nil, want permission denied for nil user")
	}
	if _, err := manager.GetAsset(ctx, accountID, userID, "asset-1"); err == nil {
		t.Fatal("GetAsset() error = nil, want permission denied for nil user")
	}
	if _, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{Name: "Server", URL: "https://server.example.com"}); err == nil {
		t.Fatal("CreateAdminResource() error = nil, want permission denied for nil user")
	}
	if len(store.savedUserResources) != 0 || store.saveServerResourceCalls != 0 {
		t.Fatalf("store was written for nil user: personal saves=%d server saves=%d", len(store.savedUserResources), store.saveServerResourceCalls)
	}
}

func TestListResourcesUsesUserGroupsNotAutoGroupsForVisibility(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{
			Id:         userID,
			AccountID:  accountID,
			AutoGroups: []string{"device-auto-group"},
			UserGroups: []string{"user-group"},
		},
		resources: []*types.Resource{
			{
				ID:            "user-group-resource",
				AccountID:     accountID,
				Scope:         types.ResourceScopeServer,
				Name:          "User Group Portal",
				URL:           "https://user-group.example.com",
				Enabled:       true,
				Visibility:    types.VisibilityRestricted,
				VisibleGroups: []string{"user-group"},
			},
			{
				ID:            "auto-group-resource",
				AccountID:     accountID,
				Scope:         types.ResourceScopeServer,
				Name:          "Auto Group Portal",
				URL:           "https://auto-group.example.com",
				Enabled:       true,
				Visibility:    types.VisibilityRestricted,
				VisibleGroups: []string{"device-auto-group"},
			},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.ServerResources) != 1 {
		t.Fatalf("server resources length = %d, want 1: %+v", len(resources.ServerResources), resources.ServerResources)
	}
	if got, want := resources.ServerResources[0].ID, "user-group-resource"; got != want {
		t.Fatalf("visible resource ID = %q, want %q", got, want)
	}
}

func TestListResourcesBuildsCategoriesFromNormalizedPersonalResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{
			{
				ID:       "personal-1",
				Name:     "Personal One",
				Category: " 个人资源 ",
				URL:      " https://one.example.com ",
				Enabled:  true,
			},
			{
				ID:       "personal-2",
				Name:     "Personal Two",
				Category: "个人资源",
				URL:      "https://two.example.com",
				Enabled:  true,
			},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if got, want := resources.PersonalResources[0].Category, "个人资源"; got != want {
		t.Fatalf("personal resource category = %q, want %q", got, want)
	}
	count := 0
	for _, category := range resources.Categories {
		if category.Name == "个人资源" {
			count++
		}
		if category.Name == " 个人资源 " {
			t.Fatalf("categories contains untrimmed personal category: %+v", resources.Categories)
		}
	}
	if count != 1 {
		t.Fatalf("personal category count = %d, want 1: %+v", count, resources.Categories)
	}
}

func TestListResourcesBuildsCategoriesFromVisibleServerResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, UserGroups: []string{"visible-group"}},
		resources: []*types.Resource{
			{
				ID:         "visible-server",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "Visible Server",
				Category:   " 企业资源 ",
				URL:        "https://visible.example.com",
				Enabled:    true,
				Visibility: types.VisibilityAll,
			},
			{
				ID:            "hidden-server",
				AccountID:     accountID,
				Scope:         types.ResourceScopeServer,
				Name:          "Hidden Server",
				Category:      "隐藏企业资源",
				URL:           "https://hidden.example.com",
				Enabled:       true,
				Visibility:    types.VisibilityRestricted,
				VisibleGroups: []string{"other-group"},
			},
			{
				ID:         "disabled-server",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "Disabled Server",
				Category:   "停用企业资源",
				URL:        "https://disabled.example.com",
				Enabled:    false,
				Visibility: types.VisibilityAll,
			},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.ServerResources) != 1 {
		t.Fatalf("server resources length = %d, want 1: %+v", len(resources.ServerResources), resources.ServerResources)
	}
	if got, want := resources.ServerResources[0].Category, "企业资源"; got != want {
		t.Fatalf("server resource category = %q, want %q", got, want)
	}
	if !hasCategory(resources.Categories, "企业资源") {
		t.Fatalf("categories missing visible server category: %+v", resources.Categories)
	}
	for _, name := range []string{"隐藏企业资源", "停用企业资源", " 企业资源 "} {
		if hasCategory(resources.Categories, name) {
			t.Fatalf("categories contains %q from hidden, disabled, or untrimmed server resource: %+v", name, resources.Categories)
		}
	}
}

func TestListResourcesIncludesStoredWorkbenchCategories(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		categories: []*types.Category{
			{ID: "stored-2", AccountID: accountID, Name: " 研发系统 ", Sort: 20},
			{ID: "stored-1", AccountID: accountID, Name: "内部系统", Sort: 10},
		},
		userResources: []types.Resource{{
			ID:       "personal-1",
			Name:     "Personal",
			Category: "个人自建",
			URL:      "https://personal.example.com",
			Enabled:  true,
		}},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if !hasCategory(resources.Categories, "内部系统") || !hasCategory(resources.Categories, "研发系统") || !hasCategory(resources.Categories, "个人自建") {
		t.Fatalf("categories missing stored or dynamic category: %+v", resources.Categories)
	}
	if resources.Categories[3].Name != "内部系统" || resources.Categories[4].Name != "研发系统" {
		t.Fatalf("stored categories order = %+v, want sort order after built-ins", resources.Categories)
	}
}

func TestAdminCategoryCRUDUsesSettingsPermissions(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	permissions := &fakePermissionsManager{allowed: true}
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		categories: []*types.Category{{
			ID:        "category-1",
			AccountID: accountID,
			Name:      "内部系统",
			Sort:      10,
			CreatedBy: "creator-1",
		}},
	}
	manager := NewManager(store, permissions)

	categories, err := manager.ListAdminCategories(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListAdminCategories() error = %v", err)
	}
	if len(categories) != 1 || categories[0].Name != "内部系统" {
		t.Fatalf("ListAdminCategories() = %+v, want stored category", categories)
	}
	if permissions.operation != operations.Read {
		t.Fatalf("permission operation = %v, want read", permissions.operation)
	}

	created, err := manager.CreateAdminCategory(ctx, accountID, userID, &types.Category{Name: " 新分类 ", Sort: 30})
	if err != nil {
		t.Fatalf("CreateAdminCategory() error = %v", err)
	}
	if created.ID == "" || created.Name != "新分类" || created.AccountID != accountID || created.CreatedBy != userID {
		t.Fatalf("created category mismatch: %+v", created)
	}

	updated, err := manager.UpdateAdminCategory(ctx, accountID, userID, "category-1", &types.Category{Name: " 更新分类 ", Sort: 40})
	if err != nil {
		t.Fatalf("UpdateAdminCategory() error = %v", err)
	}
	if updated.ID != "category-1" || updated.Name != "更新分类" || updated.CreatedBy != "creator-1" {
		t.Fatalf("updated category mismatch: %+v", updated)
	}

	if err := manager.DeleteAdminCategory(ctx, accountID, userID, "category-1"); err != nil {
		t.Fatalf("DeleteAdminCategory() error = %v", err)
	}
	if store.deleteCategoryCalls != 1 {
		t.Fatalf("delete category calls = %d, want 1", store.deleteCategoryCalls)
	}
}

func TestAdminCategoryRejectsInvalidPayloadsBeforeStoreWrite(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		categories: []*types.Category{{
			ID:        "category-1",
			AccountID: accountID,
			Name:      "内部系统",
		}},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.CreateAdminCategory(ctx, accountID, userID, nil); err == nil {
		t.Fatal("CreateAdminCategory(nil) error = nil, want invalid argument")
	}
	if _, err := manager.CreateAdminCategory(ctx, accountID, userID, &types.Category{Name: " "}); err == nil {
		t.Fatal("CreateAdminCategory(empty name) error = nil, want invalid argument")
	}
	if _, err := manager.UpdateAdminCategory(ctx, accountID, userID, " ", &types.Category{Name: "分类"}); err == nil {
		t.Fatal("UpdateAdminCategory(empty id) error = nil, want invalid argument")
	}
	if err := manager.DeleteAdminCategory(ctx, accountID, userID, " "); err == nil {
		t.Fatal("DeleteAdminCategory(empty id) error = nil, want invalid argument")
	}
	if store.saveCategoryCalls != 0 || store.deleteCategoryCalls != 0 {
		t.Fatalf("store was written for invalid payloads: save=%d delete=%d", store.saveCategoryCalls, store.deleteCategoryCalls)
	}
}

func TestListResourcesSortsServerAndPersonalResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		resources: []*types.Resource{
			{
				ID:         "server-b",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "Beta",
				URL:        "https://beta.example.com",
				Enabled:    true,
				Visibility: types.VisibilityAll,
				Sort:       10,
			},
			{
				ID:         "server-a",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "Alpha",
				URL:        "https://alpha.example.com",
				Enabled:    true,
				Visibility: types.VisibilityAll,
				Sort:       10,
			},
			{
				ID:         "server-favorite",
				AccountID:  accountID,
				Scope:      types.ResourceScopeServer,
				Name:       "Favorite",
				URL:        "https://favorite.example.com",
				Enabled:    true,
				Favorite:   true,
				Visibility: types.VisibilityAll,
				Sort:       99,
			},
		},
		userResources: []types.Resource{
			{
				ID:      "personal-b",
				Name:    "Beta Personal",
				URL:     "https://beta-personal.example.com",
				Enabled: true,
				Sort:    20,
			},
			{
				ID:       "personal-favorite",
				Name:     "Favorite Personal",
				URL:      "https://favorite-personal.example.com",
				Enabled:  true,
				Favorite: true,
				Sort:     99,
			},
			{
				ID:      "personal-a",
				Name:    "Alpha Personal",
				URL:     "https://alpha-personal.example.com",
				Enabled: true,
				Sort:    10,
			},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	serverIDs := resourceIDs(resources.ServerResources)
	if want := []string{"server-favorite", "server-a", "server-b"}; !slices.Equal(serverIDs, want) {
		t.Fatalf("server resource IDs = %v, want %v", serverIDs, want)
	}
	personalIDs := resourceIDs(resources.PersonalResources)
	if want := []string{"personal-favorite", "personal-a", "personal-b"}; !slices.Equal(personalIDs, want) {
		t.Fatalf("personal resource IDs = %v, want %v", personalIDs, want)
	}
}

func TestListResourcesFiltersDisabledPersonalResources(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{
			{
				ID:       "enabled-personal",
				Name:     "Enabled Personal",
				Category: "个人资源",
				URL:      "https://enabled.example.com",
				Enabled:  true,
			},
			{
				ID:       "disabled-personal",
				Name:     "Disabled Personal",
				Category: "隐藏分类",
				URL:      "https://disabled.example.com",
				Enabled:  false,
			},
		},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.PersonalResources) != 1 {
		t.Fatalf("personal resources length = %d, want 1: %+v", len(resources.PersonalResources), resources.PersonalResources)
	}
	if got, want := resources.PersonalResources[0].ID, "enabled-personal"; got != want {
		t.Fatalf("personal resource ID = %q, want %q", got, want)
	}
	for _, category := range resources.Categories {
		if category.Name == "隐藏分类" {
			t.Fatalf("categories contains disabled personal resource category: %+v", resources.Categories)
		}
	}
}

func TestListResourcesHidesPersonalVisibilityConfiguration(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{{
			ID:            "personal-1",
			Name:          "Personal Portal",
			Category:      "个人资源",
			URL:           "https://personal.example.com",
			Enabled:       true,
			Visibility:    types.VisibilityRestricted,
			VisibleGroups: []string{"group-1"},
			VisibleUsers:  []string{"other-user"},
			CreatedBy:     "admin-1",
		}},
	}
	manager := NewManager(store, nil)

	resources, err := manager.ListResources(ctx, accountID, userID)
	if err != nil {
		t.Fatalf("ListResources() error = %v", err)
	}
	if len(resources.PersonalResources) != 1 {
		t.Fatalf("personal resources length = %d, want 1", len(resources.PersonalResources))
	}
	got := resources.PersonalResources[0]
	if got.Visibility != "" || len(got.VisibleGroups) != 0 || len(got.VisibleUsers) != 0 || got.CreatedBy != "" {
		t.Fatalf("ListResources() leaked personal visibility fields: %+v", got)
	}
	if got.AccountID != accountID || got.UserID != userID || got.Scope != types.ResourceScopePersonal || got.Source != types.ResourceSourceUser {
		t.Fatalf("personal resource ownership mismatch after normalization: %+v", got)
	}
}

func TestCreateAdminResourceDefaultsVisibilityToAll(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	created, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:    "Portal",
		URL:     "https://example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminResource() error = %v", err)
	}
	if got, want := created.Visibility, types.VisibilityAll; got != want {
		t.Fatalf("CreateAdminResource() visibility = %q, want %q", got, want)
	}
}

func TestCreateAdminResourceAcceptsUppercaseHTTPScheme(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	created, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:    "Portal",
		URL:     "HTTPS://example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminResource() error = %v", err)
	}
	if created.URL != "HTTPS://example.com" {
		t.Fatalf("CreateAdminResource() URL = %q, want original uppercase scheme URL", created.URL)
	}
}

func TestCreateAdminResourceNormalizesVisibilityListsAndTags(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		users: map[string]*nbtypes.User{
			userID:   {Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
			"user-1": {Id: "user-1", AccountID: accountID},
			"user-2": {Id: "user-2", AccountID: accountID},
		},
		groups: map[string]*nbtypes.Group{
			"group-1": {ID: "group-1", AccountID: accountID, Type: nbtypes.GroupTypeUser},
			"group-2": {ID: "group-2", AccountID: accountID, Type: nbtypes.GroupTypeUser},
		},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	created, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:          "Portal",
		URL:           "https://example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{" group-1 ", "", "group-1", "group-2"},
		VisibleUsers:  []string{"user-1", " user-1 ", "", "user-2"},
		Tags:          []string{"门户", " 门户 ", "", "SSO"},
	})
	if err != nil {
		t.Fatalf("CreateAdminResource() error = %v", err)
	}
	if got, want := created.VisibleGroups, []string{"group-1", "group-2"}; !slices.Equal(got, want) {
		t.Fatalf("VisibleGroups = %v, want %v", got, want)
	}
	if got, want := created.VisibleUsers, []string{"user-1", "user-2"}; !slices.Equal(got, want) {
		t.Fatalf("VisibleUsers = %v, want %v", got, want)
	}
	if got, want := created.Tags, []string{"门户", "SSO"}; !slices.Equal(got, want) {
		t.Fatalf("Tags = %v, want %v", got, want)
	}
	trimmed, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:        "  Trim Portal  ",
		Category:    " 常用应用 ",
		Description: " 描述 ",
		IconURL:     " /api/workbench/assets/icon-1 ",
		IconMode:    " uploaded ",
		URL:         " https://trim.example.com ",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateAdminResource(trimmed) error = %v", err)
	}
	if trimmed.Name != "Trim Portal" ||
		trimmed.Category != "常用应用" ||
		trimmed.Description != "描述" ||
		trimmed.IconURL != "/api/workbench/assets/icon-1" ||
		trimmed.IconMode != "uploaded" ||
		trimmed.URL != "https://trim.example.com" {
		t.Fatalf("trimmed resource mismatch: %+v", trimmed)
	}

	allResource, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:          "All Portal",
		URL:           "https://all.example.com",
		Enabled:       true,
		Visibility:    types.VisibilityAll,
		VisibleGroups: []string{"group-1"},
		VisibleUsers:  []string{"user-1"},
	})
	if err != nil {
		t.Fatalf("CreateAdminResource(all) error = %v", err)
	}
	if len(allResource.VisibleGroups) != 0 || len(allResource.VisibleUsers) != 0 {
		t.Fatalf("all resource visibility lists = groups %v users %v, want empty", allResource.VisibleGroups, allResource.VisibleUsers)
	}
}

func TestCreateAdminResourceRejectsInvalidIconMode(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:     "Portal",
		URL:      "https://example.com",
		IconMode: "external",
		Enabled:  true,
	}); err == nil {
		t.Fatal("CreateAdminResource() error = nil, want invalid icon mode rejection")
	}
}

func TestCreateAdminResourceValidatesVisibleGroupsAndUsers(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	adminID := "admin-1"
	store := &workbenchMemoryStore{
		users: map[string]*nbtypes.User{
			adminID:       {Id: adminID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
			"user-1":      {Id: "user-1", AccountID: accountID},
			"other-user":  {Id: "other-user", AccountID: "other-account"},
			"missing-one": nil,
		},
		groups: map[string]*nbtypes.Group{
			"group-1":    {ID: "group-1", AccountID: accountID, Type: nbtypes.GroupTypeUser},
			"nil-group":  nil,
			"peer-group": {ID: "peer-group", AccountID: accountID, Type: nbtypes.GroupTypePeer},
		},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:          "Portal",
		URL:           "https://example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"group-1"},
		VisibleUsers:  []string{"user-1"},
	}); err != nil {
		t.Fatalf("CreateAdminResource(valid visibility) error = %v", err)
	}

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:          "Missing Group",
		URL:           "https://missing-group.example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"missing-group"},
	}); err == nil {
		t.Fatal("CreateAdminResource(missing group) error = nil, want invalid argument")
	}

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:          "Nil Group Value",
		URL:           "https://nil-group.example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"nil-group"},
	}); err == nil {
		t.Fatal("CreateAdminResource(nil group value) error = nil, want invalid argument")
	}

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:          "Peer Group",
		URL:           "https://peer-group.example.com",
		Enabled:       true,
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"peer-group"},
	}); err == nil {
		t.Fatal("CreateAdminResource(peer group) error = nil, want invalid argument")
	}

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:         "Nil User Value",
		URL:          "https://nil-user.example.com",
		Enabled:      true,
		Visibility:   types.VisibilityRestricted,
		VisibleUsers: []string{"missing-one"},
	}); err == nil {
		t.Fatal("CreateAdminResource(nil user value) error = nil, want invalid argument")
	}

	if _, err := manager.CreateAdminResource(ctx, accountID, adminID, &types.Resource{
		Name:         "Other User",
		URL:          "https://other-user.example.com",
		Enabled:      true,
		Visibility:   types.VisibilityRestricted,
		VisibleUsers: []string{"other-user"},
	}); err == nil {
		t.Fatal("CreateAdminResource(other account user) error = nil, want invalid argument")
	}
}

func TestCreateAdminResourceIgnoresClientProvidedID(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		resources: []*types.Resource{{
			ID:         "client-provided-id",
			AccountID:  accountID,
			Name:       "Existing Portal",
			URL:        "https://existing.example.com",
			Scope:      types.ResourceScopeServer,
			Source:     types.ResourceSourceAdmin,
			Visibility: types.VisibilityAll,
			Enabled:    true,
		}},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	created, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		ID:      "client-provided-id",
		Name:    "New Portal",
		URL:     "https://new.example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminResource() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateAdminResource() ID is empty")
	}
	if created.ID == "client-provided-id" {
		t.Fatal("CreateAdminResource() reused client-provided ID")
	}
	if len(store.resources) != 2 {
		t.Fatalf("server resources length = %d, want 2", len(store.resources))
	}
	if store.resources[0].ID != "client-provided-id" || store.resources[0].Name != "Existing Portal" {
		t.Fatalf("existing resource was overwritten: %+v", store.resources[0])
	}
	if store.resources[1].ID != created.ID || store.resources[1].Name != "New Portal" {
		t.Fatalf("created resource mismatch: %+v", store.resources[1])
	}
}

func TestCreateAdminResourceFailsWhenSavedResourceCannotBeReloaded(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user:                       &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		nilServerResourceAfterSave: true,
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.CreateAdminResource(ctx, accountID, userID, &types.Resource{
		Name:    "New Portal",
		URL:     "https://new.example.com",
		Enabled: true,
	}); err == nil {
		t.Fatal("CreateAdminResource() error = nil, want reload failure")
	}
	if store.saveServerResourceCalls != 1 {
		t.Fatalf("SaveWorkbenchServerResource() calls = %d, want 1", store.saveServerResourceCalls)
	}
}

func TestUpdateAdminResourcePreservesCreatedByAndRequiresExistingResource(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	creatorID := "creator-1"
	editorID := "editor-1"
	existing := &types.Resource{
		ID:         "resource-1",
		AccountID:  accountID,
		Name:       "Portal",
		URL:        "https://example.com",
		Scope:      types.ResourceScopeServer,
		Source:     types.ResourceSourceAdmin,
		Visibility: types.VisibilityAll,
		CreatedBy:  creatorID,
		Enabled:    true,
	}
	store := &workbenchMemoryStore{
		user:      &nbtypes.User{Id: editorID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		resources: []*types.Resource{existing},
		groups: map[string]*nbtypes.Group{
			"group-1": {ID: "group-1", AccountID: accountID, Type: nbtypes.GroupTypeUser},
		},
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	updated, err := manager.UpdateAdminResource(ctx, accountID, editorID, existing.ID, &types.Resource{
		Name:          "Portal Updated",
		URL:           "https://updated.example.com",
		Visibility:    types.VisibilityRestricted,
		VisibleGroups: []string{"group-1"},
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("UpdateAdminResource() error = %v", err)
	}
	if updated.CreatedBy != creatorID {
		t.Fatalf("CreatedBy = %q, want original creator %q", updated.CreatedBy, creatorID)
	}
	if updated.Name != "Portal Updated" || updated.URL != "https://updated.example.com" {
		t.Fatalf("updated resource fields mismatch: %+v", updated)
	}
	if len(updated.VisibleGroups) != 1 || updated.VisibleGroups[0] != "group-1" {
		t.Fatalf("VisibleGroups = %+v, want group-1", updated.VisibleGroups)
	}

	if _, err := manager.UpdateAdminResource(ctx, accountID, editorID, "missing-resource", &types.Resource{
		Name:    "Missing",
		URL:     "https://missing.example.com",
		Enabled: true,
	}); err == nil {
		t.Fatal("UpdateAdminResource() error = nil, want not found for missing resource")
	}
}

func TestUpdateAdminResourceFailsWhenSavedResourceCannotBeReloaded(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	existing := &types.Resource{
		ID:         "resource-1",
		AccountID:  accountID,
		Name:       "Portal",
		URL:        "https://example.com",
		Scope:      types.ResourceScopeServer,
		Source:     types.ResourceSourceAdmin,
		Visibility: types.VisibilityAll,
		CreatedBy:  userID,
		Enabled:    true,
	}
	store := &workbenchMemoryStore{
		user:                       &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		resources:                  []*types.Resource{existing},
		nilServerResourceAfterSave: true,
	}
	manager := NewManager(store, &fakePermissionsManager{allowed: true})

	if _, err := manager.UpdateAdminResource(ctx, accountID, userID, existing.ID, &types.Resource{
		Name:    "Portal Updated",
		URL:     "https://updated.example.com",
		Enabled: true,
	}); err == nil {
		t.Fatal("UpdateAdminResource() error = nil, want reload failure")
	}
	if store.saveServerResourceCalls != 1 {
		t.Fatalf("SaveWorkbenchServerResource() calls = %d, want 1", store.saveServerResourceCalls)
	}
}

func TestCreatePersonalResourceIgnoresClientProvidedID(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{{
			ID:      "client-provided-id",
			Name:    "Existing Personal",
			URL:     "https://existing.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	created, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		ID:      "client-provided-id",
		Name:    "New Personal",
		URL:     "https://new.example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreatePersonalResource() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreatePersonalResource() ID is empty")
	}
	if created.ID == "client-provided-id" {
		t.Fatal("CreatePersonalResource() reused client-provided ID")
	}
	if len(store.savedUserResources) != 2 {
		t.Fatalf("saved personal resources length = %d, want 2", len(store.savedUserResources))
	}
	if store.savedUserResources[0].ID != "client-provided-id" || store.savedUserResources[1].ID != created.ID {
		t.Fatalf("saved personal resource IDs = %q/%q, want existing/new ID %q", store.savedUserResources[0].ID, store.savedUserResources[1].ID, created.ID)
	}
	if created.AccountID != accountID || created.UserID != userID || created.Scope != types.ResourceScopePersonal || created.Source != types.ResourceSourceUser {
		t.Fatalf("created personal resource ownership mismatch: %+v", created)
	}
	if !created.Enabled {
		t.Fatal("CreatePersonalResource() enabled = false, want default true")
	}
}

func TestPersonalResourceOperationsRejectUserFromDifferentAccount(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: "other-account"},
		userResources: []types.Resource{{
			ID:      "personal-1",
			Name:    "Existing Personal",
			URL:     "https://existing.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	if _, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:    "New Personal",
		URL:     "https://new.example.com",
		Enabled: true,
	}); err == nil {
		t.Fatal("CreatePersonalResource() error = nil, want permission denied")
	}

	if _, err := manager.UpdatePersonalResource(ctx, accountID, userID, "personal-1", &types.Resource{
		Name:    "Updated Personal",
		URL:     "https://updated.example.com",
		Enabled: true,
	}); err == nil {
		t.Fatal("UpdatePersonalResource() error = nil, want permission denied")
	}

	if err := manager.DeletePersonalResource(ctx, accountID, userID, "personal-1"); err == nil {
		t.Fatal("DeletePersonalResource() error = nil, want permission denied")
	}

	if len(store.savedUserResources) != 0 {
		t.Fatalf("saved user resources length = %d, want 0", len(store.savedUserResources))
	}
}

func TestCreatePersonalResourceNormalizesFields(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{user: &nbtypes.User{Id: userID, AccountID: accountID}}
	manager := NewManager(store, nil)

	created, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:        "  个人系统  ",
		Category:    " 个人资源 ",
		Description: " 描述 ",
		IconURL:     " /api/workbench/assets/icon-1 ",
		IconMode:    " fetched ",
		URL:         " https://personal.example.com ",
		Tags:        []string{" 常用 ", "常用", ""},
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreatePersonalResource() error = %v", err)
	}
	if created.Name != "个人系统" ||
		created.Category != "个人资源" ||
		created.Description != "描述" ||
		created.IconURL != "/api/workbench/assets/icon-1" ||
		created.IconMode != "fetched" ||
		created.URL != "https://personal.example.com" {
		t.Fatalf("trimmed personal resource mismatch: %+v", created)
	}
	if got, want := created.Tags, []string{"常用"}; !slices.Equal(got, want) {
		t.Fatalf("Tags = %v, want %v", got, want)
	}
}

func TestPersonalResourceClearsCreatedBy(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{{
			ID:      "personal-1",
			Name:    "Existing Personal",
			URL:     "https://existing.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	created, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:      "New Personal",
		URL:       "https://new.example.com",
		Enabled:   true,
		CreatedBy: "admin-1",
	})
	if err != nil {
		t.Fatalf("CreatePersonalResource() error = %v", err)
	}
	if created.CreatedBy != "" {
		t.Fatalf("CreatePersonalResource() CreatedBy = %q, want empty", created.CreatedBy)
	}
	if store.savedUserResources[len(store.savedUserResources)-1].CreatedBy != "" {
		t.Fatalf("saved created personal CreatedBy = %q, want empty", store.savedUserResources[len(store.savedUserResources)-1].CreatedBy)
	}

	updated, err := manager.UpdatePersonalResource(ctx, accountID, userID, "personal-1", &types.Resource{
		Name:      "Updated Personal",
		URL:       "https://updated.example.com",
		Enabled:   true,
		CreatedBy: "admin-1",
	})
	if err != nil {
		t.Fatalf("UpdatePersonalResource() error = %v", err)
	}
	if updated.CreatedBy != "" {
		t.Fatalf("UpdatePersonalResource() CreatedBy = %q, want empty", updated.CreatedBy)
	}
	if store.savedUserResources[0].CreatedBy != "" {
		t.Fatalf("saved updated personal CreatedBy = %q, want empty", store.savedUserResources[0].CreatedBy)
	}
}

func TestCreatePersonalResourceAcceptsUppercaseHTTPScheme(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{user: &nbtypes.User{Id: userID, AccountID: accountID}}
	manager := NewManager(store, nil)

	created, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:    "Personal",
		URL:     "HTTPS://personal.example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreatePersonalResource() error = %v", err)
	}
	if created.URL != "HTTPS://personal.example.com" {
		t.Fatalf("CreatePersonalResource() URL = %q, want original uppercase scheme URL", created.URL)
	}
}

func TestCreatePersonalResourceValidatesIconMode(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{user: &nbtypes.User{Id: userID, AccountID: accountID}}
	manager := NewManager(store, nil)

	defaulted, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:    "Personal",
		URL:     "https://personal.example.com",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreatePersonalResource(default icon mode) error = %v", err)
	}
	if got, want := defaulted.IconMode, types.IconModeLetter; got != want {
		t.Fatalf("IconMode = %q, want %q", got, want)
	}

	if _, err := manager.CreatePersonalResource(ctx, accountID, userID, &types.Resource{
		Name:     "Personal Invalid",
		URL:      "https://invalid.example.com",
		IconMode: "external",
		Enabled:  true,
	}); err == nil {
		t.Fatal("CreatePersonalResource(invalid icon mode) error = nil, want rejection")
	}
}

func TestUpdatePersonalResourceKeepsResourceEnabled(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
		userResources: []types.Resource{{
			ID:      "personal-1",
			Name:    "Existing Personal",
			URL:     "https://existing.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	updated, err := manager.UpdatePersonalResource(ctx, accountID, userID, "personal-1", &types.Resource{
		Name:    "Updated Personal",
		URL:     "https://updated.example.com",
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("UpdatePersonalResource() error = %v", err)
	}
	if !updated.Enabled {
		t.Fatal("UpdatePersonalResource() enabled = false, want true")
	}
	if len(store.savedUserResources) != 1 || !store.savedUserResources[0].Enabled {
		t.Fatalf("saved personal resources = %+v, want enabled updated resource", store.savedUserResources)
	}
}

func TestManagerRejectsEmptyResourceIDsBeforeStoreAccess(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		userResources: []types.Resource{{
			ID:      "personal-1",
			Name:    "Personal",
			URL:     "https://personal.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	if _, err := manager.GetAdminResource(ctx, accountID, userID, " "); err == nil {
		t.Fatal("GetAdminResource() error = nil, want empty ID rejection")
	}
	if _, err := manager.UpdateAdminResource(ctx, accountID, userID, "", &types.Resource{Name: "Server", URL: "https://server.example.com"}); err == nil {
		t.Fatal("UpdateAdminResource() error = nil, want empty ID rejection")
	}
	if err := manager.DeleteAdminResource(ctx, accountID, userID, "\t"); err == nil {
		t.Fatal("DeleteAdminResource() error = nil, want empty ID rejection")
	}
	if _, err := manager.UpdatePersonalResource(ctx, accountID, userID, " ", &types.Resource{Name: "Personal", URL: "https://personal.example.com"}); err == nil {
		t.Fatal("UpdatePersonalResource() error = nil, want empty ID rejection")
	}
	if err := manager.DeletePersonalResource(ctx, accountID, userID, ""); err == nil {
		t.Fatal("DeletePersonalResource() error = nil, want empty ID rejection")
	}

	if store.getServerResourceCalls != 0 || store.deleteServerResourceCalls != 0 || len(store.savedUserResources) != 0 {
		t.Fatalf("store was touched for empty resource IDs: get=%d delete=%d saved=%d", store.getServerResourceCalls, store.deleteServerResourceCalls, len(store.savedUserResources))
	}
}

func TestManagerRejectsNilResourcePayloadsBeforeStoreWrite(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleAdmin},
		resources: []*types.Resource{{
			ID:         "server-1",
			AccountID:  accountID,
			Name:       "Server",
			URL:        "https://server.example.com",
			Enabled:    true,
			Visibility: types.VisibilityAll,
		}},
		userResources: []types.Resource{{
			ID:      "personal-1",
			Name:    "Personal",
			URL:     "https://personal.example.com",
			Enabled: true,
		}},
	}
	manager := NewManager(store, nil)

	if _, err := manager.CreateAdminResource(ctx, accountID, userID, nil); err == nil {
		t.Fatal("CreateAdminResource() error = nil, want nil payload rejection")
	}
	if _, err := manager.UpdateAdminResource(ctx, accountID, userID, "server-1", nil); err == nil {
		t.Fatal("UpdateAdminResource() error = nil, want nil payload rejection")
	}
	if _, err := manager.CreatePersonalResource(ctx, accountID, userID, nil); err == nil {
		t.Fatal("CreatePersonalResource() error = nil, want nil payload rejection")
	}
	if _, err := manager.UpdatePersonalResource(ctx, accountID, userID, "personal-1", nil); err == nil {
		t.Fatal("UpdatePersonalResource() error = nil, want nil payload rejection")
	}

	if store.saveServerResourceCalls != 0 || len(store.savedUserResources) != 0 {
		t.Fatalf("store was written for nil payloads: server saves=%d personal saves=%d", store.saveServerResourceCalls, len(store.savedUserResources))
	}
}

func TestSaveAdminIconAssetUsesSettingsCreateOrUpdatePermission(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "admin-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID, Role: nbtypes.UserRoleUser},
	}
	permissions := &fakePermissionsManager{allowedOperations: map[operations.Operation]bool{operations.Update: true}}
	manager := NewManager(store, permissions)

	asset, err := manager.SaveAdminIconAsset(ctx, accountID, userID, "asset-1", "", "/api/workbench/assets/asset-1", "/tmp/asset-1", "image/png", "sha", 10)
	if err != nil {
		t.Fatalf("SaveAdminIconAsset() error = %v", err)
	}
	if asset.OwnerUserID != "" {
		t.Fatalf("SaveAdminIconAsset() owner user ID = %q, want empty", asset.OwnerUserID)
	}
	if store.asset == nil || store.asset.ID != "asset-1" {
		t.Fatalf("SaveAdminIconAsset() did not save asset: %+v", store.asset)
	}
	if got, want := permissions.operations, []operations.Operation{operations.Create, operations.Update}; !slices.Equal(got, want) {
		t.Fatalf("ValidateUserPermissions() operations = %v, want %v", got, want)
	}

	permissions.allowedOperations = map[operations.Operation]bool{}
	if _, err := manager.SaveAdminIconAsset(ctx, accountID, userID, "asset-2", "", "/api/workbench/assets/asset-2", "/tmp/asset-2", "image/png", "sha", 10); err == nil {
		t.Fatal("SaveAdminIconAsset() error = nil, want permission denied")
	}
}

func TestEnsurePersonalAssetAccessRequiresUserInAccount(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	manager := NewManager(&workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
	}, nil)

	if err := manager.EnsurePersonalAssetAccess(ctx, accountID, userID); err != nil {
		t.Fatalf("EnsurePersonalAssetAccess() error = %v", err)
	}

	otherManager := NewManager(&workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: "other-account"},
	}, nil)
	if err := otherManager.EnsurePersonalAssetAccess(ctx, accountID, userID); err == nil {
		t.Fatal("EnsurePersonalAssetAccess() error = nil, want permission denied")
	}
}

func TestSavePersonalIconAssetRequiresUserInAccount(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
	}
	manager := NewManager(store, nil)

	asset, err := manager.SavePersonalIconAsset(ctx, accountID, userID, "asset-1", "", "/api/workbench/assets/asset-1", "/tmp/asset-1", "image/png", "sha", 10)
	if err != nil {
		t.Fatalf("SavePersonalIconAsset() error = %v", err)
	}
	if asset.OwnerUserID != userID {
		t.Fatalf("SavePersonalIconAsset() owner user ID = %q, want %q", asset.OwnerUserID, userID)
	}
	if store.asset == nil || store.asset.ID != "asset-1" {
		t.Fatalf("SavePersonalIconAsset() did not save asset: %+v", store.asset)
	}

	otherStore := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: "other-account"},
	}
	otherManager := NewManager(otherStore, nil)
	if _, err := otherManager.SavePersonalIconAsset(ctx, accountID, userID, "asset-2", "", "/api/workbench/assets/asset-2", "/tmp/asset-2", "image/png", "sha", 10); err == nil {
		t.Fatal("SavePersonalIconAsset() error = nil, want permission denied")
	}
	if otherStore.asset != nil {
		t.Fatalf("SavePersonalIconAsset() saved cross-account asset: %+v", otherStore.asset)
	}
}

func TestGetAssetAllowsOnlyOwnerForPersonalAssets(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	asset := &types.Asset{
		ID:          "asset-1",
		AccountID:   accountID,
		OwnerUserID: "owner-user",
		PublicURL:   "/api/workbench/assets/asset-1",
	}
	store := &workbenchMemoryStore{
		users: map[string]*nbtypes.User{
			"owner-user": {Id: "owner-user", AccountID: accountID},
			"other-user": {Id: "other-user", AccountID: accountID},
		},
		asset: asset,
	}
	manager := NewManager(store, nil)

	if _, err := manager.GetAsset(ctx, accountID, "other-user", asset.ID); err == nil {
		t.Fatal("GetAsset() error = nil, want not found for non-owner")
	}
	got, err := manager.GetAsset(ctx, accountID, "owner-user", asset.ID)
	if err != nil {
		t.Fatalf("GetAsset() owner error = %v", err)
	}
	if got.ID != asset.ID {
		t.Fatalf("GetAsset() ID = %q, want %q", got.ID, asset.ID)
	}

	crossAccountStore := &workbenchMemoryStore{
		user:  &nbtypes.User{Id: "owner-user", AccountID: "other-account"},
		asset: asset,
	}
	crossAccountManager := NewManager(crossAccountStore, nil)
	if _, err := crossAccountManager.GetAsset(ctx, accountID, "owner-user", asset.ID); err == nil {
		t.Fatal("GetAsset() cross-account error = nil, want permission denied")
	}
}

func TestGetAssetRequiresUserInAccountForAccountAssets(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	asset := &types.Asset{
		ID:          "asset-1",
		AccountID:   accountID,
		OwnerUserID: "",
		PublicURL:   "/api/workbench/assets/asset-1",
	}
	store := &workbenchMemoryStore{
		user:  &nbtypes.User{Id: "user-1", AccountID: accountID},
		asset: asset,
	}
	manager := NewManager(store, nil)

	if _, err := manager.GetAsset(ctx, accountID, "user-1", asset.ID); err != nil {
		t.Fatalf("GetAsset() account asset error = %v", err)
	}

	crossAccountStore := &workbenchMemoryStore{
		user:  &nbtypes.User{Id: "user-1", AccountID: "other-account"},
		asset: asset,
	}
	crossAccountManager := NewManager(crossAccountStore, nil)
	if _, err := crossAccountManager.GetAsset(ctx, accountID, "user-1", asset.ID); err == nil {
		t.Fatal("GetAsset() cross-account account asset error = nil, want permission denied")
	}

	crossAccountAssetStore := &workbenchMemoryStore{
		user: &nbtypes.User{Id: "user-1", AccountID: accountID},
		asset: &types.Asset{
			ID:          asset.ID,
			AccountID:   "other-account",
			OwnerUserID: "",
			PublicURL:   asset.PublicURL,
		},
	}
	crossAccountAssetManager := NewManager(crossAccountAssetStore, nil)
	if _, err := crossAccountAssetManager.GetAsset(ctx, accountID, "user-1", asset.ID); err == nil {
		t.Fatal("GetAsset() cross-account returned asset error = nil, want not found")
	}
}

func TestGetAssetRejectsEmptyAssetIDBeforeStoreAccess(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user:  &nbtypes.User{Id: userID, AccountID: accountID},
		asset: &types.Asset{ID: "asset-1", AccountID: accountID},
	}
	manager := NewManager(store, nil)

	if _, err := manager.GetAsset(ctx, accountID, userID, " "); err == nil {
		t.Fatal("GetAsset() error = nil, want empty asset ID rejection")
	}
	if store.getAssetCalls != 0 {
		t.Fatalf("GetWorkbenchAsset calls = %d, want 0", store.getAssetCalls)
	}
}

func TestGetAssetTreatsNilStoreResultAsNotFound(t *testing.T) {
	ctx := context.Background()
	accountID := "account-1"
	userID := "user-1"
	store := &workbenchMemoryStore{
		user: &nbtypes.User{Id: userID, AccountID: accountID},
	}
	manager := NewManager(store, nil)

	if _, err := manager.GetAsset(ctx, accountID, userID, "missing-asset"); err == nil {
		t.Fatal("GetAsset() error = nil, want not found for nil store asset")
	}
	if store.getAssetCalls != 1 {
		t.Fatalf("GetWorkbenchAsset calls = %d, want 1", store.getAssetCalls)
	}
}

func hasCategory(categories []types.Category, name string) bool {
	for _, category := range categories {
		if category.Name == name {
			return true
		}
	}
	return false
}

func resourceIDs(resources []types.Resource) []string {
	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		ids = append(ids, resource.ID)
	}
	return ids
}

type fakePermissionsManager struct {
	allowed           bool
	allowedOperations map[operations.Operation]bool
	module            modules.Module
	operation         operations.Operation
	operations        []operations.Operation
}

func (m *fakePermissionsManager) ValidateUserPermissions(ctx context.Context, accountID, userID string, module modules.Module, operation operations.Operation) (bool, context.Context, error) {
	m.module = module
	m.operation = operation
	m.operations = append(m.operations, operation)
	if m.allowedOperations != nil {
		return m.allowedOperations[operation], ctx, nil
	}
	return m.allowed, ctx, nil
}

type workbenchMemoryStore struct {
	user                       *nbtypes.User
	users                      map[string]*nbtypes.User
	groups                     map[string]*nbtypes.Group
	resources                  []*types.Resource
	categories                 []*types.Category
	userResources              []types.Resource
	savedUserResources         []types.Resource
	savedCategory              *types.Category
	recentVisits               []types.RecentVisit
	savedRecentVisits          []types.RecentVisit
	asset                      *types.Asset
	forceNilServerResource     bool
	nilServerResourceAfterSave bool
	getServerResourceCalls     int
	saveServerResourceCalls    int
	deleteServerResourceCalls  int
	getCategoryCalls           int
	saveCategoryCalls          int
	deleteCategoryCalls        int
	getAssetCalls              int
}

type fakeWorkbenchEventStore struct {
	initiatorID string
	targetID    string
	accountID   string
	activity    activity.Activity
	meta        map[string]any
}

func (s *fakeWorkbenchEventStore) StoreEvent(ctx context.Context, initiatorID, targetID, accountID string, activityID activity.ActivityDescriber, meta map[string]any) {
	s.initiatorID = initiatorID
	s.targetID = targetID
	s.accountID = accountID
	s.activity = activityID.(activity.Activity)
	s.meta = meta
}

func (s *workbenchMemoryStore) GetUserByUserID(ctx context.Context, lockStrength store.LockingStrength, userID string) (*nbtypes.User, error) {
	if s.users != nil {
		user := s.users[userID]
		if user == nil {
			return nil, status.Errorf(status.NotFound, "user not found")
		}
		return user, nil
	}
	return s.user, nil
}

func (s *workbenchMemoryStore) GetGroupsByIDs(ctx context.Context, lockStrength store.LockingStrength, accountID string, groupIDs []string) (map[string]*nbtypes.Group, error) {
	result := make(map[string]*nbtypes.Group)
	for _, groupID := range groupIDs {
		group := s.groups[groupID]
		if group != nil && group.AccountID == accountID {
			result[groupID] = group
		}
	}
	return result, nil
}

func (s *workbenchMemoryStore) GetWorkbenchServerResources(ctx context.Context, accountID string) ([]*types.Resource, error) {
	return s.resources, nil
}

func (s *workbenchMemoryStore) GetWorkbenchServerResource(ctx context.Context, accountID, resourceID string) (*types.Resource, error) {
	s.getServerResourceCalls++
	if s.forceNilServerResource || (s.nilServerResourceAfterSave && s.saveServerResourceCalls > 0) {
		return nil, nil
	}
	for _, resource := range s.resources {
		if resource.ID == resourceID {
			return resource, nil
		}
	}
	return nil, nil
}

func (s *workbenchMemoryStore) SaveWorkbenchServerResource(ctx context.Context, resource *types.Resource) error {
	s.saveServerResourceCalls++
	for i := range s.resources {
		if s.resources[i].ID == resource.ID {
			s.resources[i] = resource
			return nil
		}
	}
	s.resources = append(s.resources, resource)
	return nil
}

func (s *workbenchMemoryStore) DeleteWorkbenchServerResource(ctx context.Context, accountID, resourceID string) error {
	s.deleteServerResourceCalls++
	return nil
}

func (s *workbenchMemoryStore) GetWorkbenchCategories(ctx context.Context, accountID string) ([]*types.Category, error) {
	return s.categories, nil
}

func (s *workbenchMemoryStore) GetWorkbenchCategory(ctx context.Context, accountID, categoryID string) (*types.Category, error) {
	s.getCategoryCalls++
	for _, category := range s.categories {
		if category.ID == categoryID {
			return category, nil
		}
	}
	if s.savedCategory != nil && s.savedCategory.ID == categoryID {
		return s.savedCategory, nil
	}
	return nil, nil
}

func (s *workbenchMemoryStore) SaveWorkbenchCategory(ctx context.Context, category *types.Category) error {
	s.saveCategoryCalls++
	copyCategory := *category
	s.savedCategory = &copyCategory
	for i := range s.categories {
		if s.categories[i].ID == category.ID {
			s.categories[i] = &copyCategory
			return nil
		}
	}
	s.categories = append(s.categories, &copyCategory)
	return nil
}

func (s *workbenchMemoryStore) DeleteWorkbenchCategory(ctx context.Context, accountID, categoryID string) error {
	s.deleteCategoryCalls++
	return nil
}

func (s *workbenchMemoryStore) GetWorkbenchUserResources(ctx context.Context, accountID, userID string) ([]types.Resource, error) {
	return s.userResources, nil
}

func (s *workbenchMemoryStore) GetWorkbenchUserState(ctx context.Context, accountID, userID string) (*types.UserResources, error) {
	return &types.UserResources{
		AccountID:     accountID,
		UserID:        userID,
		ResourcesJSON: append([]types.Resource(nil), s.userResources...),
		RecentVisits:  append([]types.RecentVisit(nil), s.recentVisits...),
	}, nil
}

func (s *workbenchMemoryStore) SaveWorkbenchUserResources(ctx context.Context, accountID, userID string, resources []types.Resource) error {
	s.savedUserResources = append([]types.Resource(nil), resources...)
	s.userResources = append([]types.Resource(nil), resources...)
	return nil
}

func (s *workbenchMemoryStore) SaveWorkbenchUserRecentVisits(ctx context.Context, accountID, userID string, recentVisits []types.RecentVisit) error {
	s.savedRecentVisits = append([]types.RecentVisit(nil), recentVisits...)
	s.recentVisits = append([]types.RecentVisit(nil), recentVisits...)
	return nil
}

func (s *workbenchMemoryStore) SaveWorkbenchAsset(ctx context.Context, asset *types.Asset) error {
	s.asset = asset
	return nil
}

func (s *workbenchMemoryStore) GetWorkbenchAsset(ctx context.Context, accountID, assetID string) (*types.Asset, error) {
	s.getAssetCalls++
	return s.asset, nil
}

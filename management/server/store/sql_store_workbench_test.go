package store

import (
	"context"
	"testing"
	"time"

	workbenchTypes "github.com/netbirdio/netbird/management/server/workbench/types"
)

func TestSqlStoreAutoMigratesWorkbenchTables(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	tables := []struct {
		name  string
		model any
	}{
		{name: "workbench_resources", model: &workbenchTypes.Resource{}},
		{name: "workbench_resource_visible_groups", model: &workbenchTypes.ResourceVisibleGroup{}},
		{name: "workbench_resource_visible_users", model: &workbenchTypes.ResourceVisibleUser{}},
		{name: "workbench_user_resources", model: &workbenchTypes.UserResources{}},
		{name: "workbench_assets", model: &workbenchTypes.Asset{}},
		{name: "workbench_categories", model: &workbenchTypes.Category{}},
	}
	for _, table := range tables {
		t.Run(table.name, func(t *testing.T) {
			if !sqlStore.db.Migrator().HasTable(table.model) {
				t.Fatalf("table %s was not auto-migrated", table.name)
			}
		})
	}
	if !sqlStore.db.Migrator().HasColumn(&workbenchTypes.UserResources{}, "recent_visits") {
		t.Fatal("workbench_user_resources.recent_visits was not auto-migrated")
	}
}

func TestSqlStoreWorkbenchUserResourcesRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	resources := []workbenchTypes.Resource{{
		ID:       "personal-1",
		Name:     "个人系统",
		Category: "个人资源",
		URL:      "https://example.com",
		Tags:     []string{"常用", "中文"},
		Metadata: map[string]any{"source": "test"},
		Scope:    workbenchTypes.ResourceScopePersonal,
		Source:   workbenchTypes.ResourceSourceUser,
		Enabled:  true,
	}}

	if err := sqlStore.SaveWorkbenchUserResources(ctx, "account-1", "user-1", resources); err != nil {
		t.Fatalf("SaveWorkbenchUserResources() error = %v", err)
	}
	got, err := sqlStore.GetWorkbenchUserResources(ctx, "account-1", "user-1")
	if err != nil {
		t.Fatalf("GetWorkbenchUserResources() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("resources length = %d, want 1", len(got))
	}
	if got[0].Name != "个人系统" || got[0].Tags[1] != "中文" || got[0].Metadata["source"] != "test" {
		t.Fatalf("resources round trip mismatch: %+v", got[0])
	}

	empty, err := sqlStore.GetWorkbenchUserResources(ctx, "account-1", "user-2")
	if err != nil {
		t.Fatalf("GetWorkbenchUserResources() empty user error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty user resources length = %d, want 0", len(empty))
	}
}

func TestSqlStoreWorkbenchUserRecentVisitsPreservePersonalResources(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	resources := []workbenchTypes.Resource{{
		ID:      "personal-1",
		Name:    "个人系统",
		URL:     "https://example.com",
		Scope:   workbenchTypes.ResourceScopePersonal,
		Source:  workbenchTypes.ResourceSourceUser,
		Enabled: true,
	}}
	if err := sqlStore.SaveWorkbenchUserResources(ctx, "account-1", "user-1", resources); err != nil {
		t.Fatalf("SaveWorkbenchUserResources() error = %v", err)
	}
	visitedAt := time.Now().UTC().Truncate(time.Second)
	if err := sqlStore.SaveWorkbenchUserRecentVisits(ctx, "account-1", "user-1", []workbenchTypes.RecentVisit{{
		ResourceID: "server-1",
		Scope:      workbenchTypes.ResourceScopeServer,
		VisitedAt:  visitedAt,
	}}); err != nil {
		t.Fatalf("SaveWorkbenchUserRecentVisits() error = %v", err)
	}
	state, err := sqlStore.GetWorkbenchUserState(ctx, "account-1", "user-1")
	if err != nil {
		t.Fatalf("GetWorkbenchUserState() error = %v", err)
	}
	if len(state.ResourcesJSON) != 1 || state.ResourcesJSON[0].ID != "personal-1" {
		t.Fatalf("resources were not preserved: %+v", state.ResourcesJSON)
	}
	if len(state.RecentVisits) != 1 ||
		state.RecentVisits[0].ResourceID != "server-1" ||
		state.RecentVisits[0].Scope != workbenchTypes.ResourceScopeServer ||
		!state.RecentVisits[0].VisitedAt.Equal(visitedAt) {
		t.Fatalf("recent visits mismatch: %+v", state.RecentVisits)
	}
}

func TestSqlStoreWorkbenchServerResourceVisibilityRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	resource := &workbenchTypes.Resource{
		ID:            "server-1",
		AccountID:     "account-1",
		Name:          "企业门户",
		URL:           "https://portal.example.com",
		Scope:         workbenchTypes.ResourceScopeServer,
		Source:        workbenchTypes.ResourceSourceAdmin,
		Visibility:    workbenchTypes.VisibilityRestricted,
		VisibleGroups: []string{"group-2", "group-1"},
		VisibleUsers:  []string{"user-2", "user-1"},
		Enabled:       true,
	}

	if err := sqlStore.SaveWorkbenchServerResource(ctx, resource); err != nil {
		t.Fatalf("SaveWorkbenchServerResource() error = %v", err)
	}
	got, err := sqlStore.GetWorkbenchServerResource(ctx, "account-1", "server-1")
	if err != nil {
		t.Fatalf("GetWorkbenchServerResource() error = %v", err)
	}
	if got.Visibility != workbenchTypes.VisibilityRestricted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, workbenchTypes.VisibilityRestricted)
	}
	if len(got.VisibleGroups) != 2 || got.VisibleGroups[0] != "group-1" || got.VisibleGroups[1] != "group-2" {
		t.Fatalf("visible groups = %+v, want stable group-1/group-2 order", got.VisibleGroups)
	}
	if len(got.VisibleUsers) != 2 || got.VisibleUsers[0] != "user-1" || got.VisibleUsers[1] != "user-2" {
		t.Fatalf("visible users = %+v, want stable user-1/user-2 order", got.VisibleUsers)
	}

	resource.VisibleGroups = []string{"group-3"}
	resource.VisibleUsers = nil
	if err := sqlStore.SaveWorkbenchServerResource(ctx, resource); err != nil {
		t.Fatalf("SaveWorkbenchServerResource() update error = %v", err)
	}
	updated, err := sqlStore.GetWorkbenchServerResource(ctx, "account-1", "server-1")
	if err != nil {
		t.Fatalf("GetWorkbenchServerResource() updated error = %v", err)
	}
	if len(updated.VisibleGroups) != 1 || updated.VisibleGroups[0] != "group-3" {
		t.Fatalf("updated visible groups = %+v, want group-3", updated.VisibleGroups)
	}
	if len(updated.VisibleUsers) != 0 {
		t.Fatalf("updated visible users = %+v, want empty", updated.VisibleUsers)
	}

	resource.Visibility = workbenchTypes.VisibilityAll
	resource.VisibleGroups = nil
	resource.VisibleUsers = nil
	if err := sqlStore.SaveWorkbenchServerResource(ctx, resource); err != nil {
		t.Fatalf("SaveWorkbenchServerResource() clear visibility error = %v", err)
	}
	allResource, err := sqlStore.GetWorkbenchServerResource(ctx, "account-1", "server-1")
	if err != nil {
		t.Fatalf("GetWorkbenchServerResource() all visibility error = %v", err)
	}
	if allResource.Visibility != workbenchTypes.VisibilityAll {
		t.Fatalf("visibility = %q, want %q", allResource.Visibility, workbenchTypes.VisibilityAll)
	}
	if len(allResource.VisibleGroups) != 0 || len(allResource.VisibleUsers) != 0 {
		t.Fatalf("all visibility relations = groups %+v users %+v, want empty", allResource.VisibleGroups, allResource.VisibleUsers)
	}

	var relationGroups []workbenchTypes.ResourceVisibleGroup
	if err := sqlStore.db.Where("account_id = ? AND resource_id = ?", "account-1", "server-1").Find(&relationGroups).Error; err != nil {
		t.Fatalf("query cleared visible groups error = %v", err)
	}
	if len(relationGroups) != 0 {
		t.Fatalf("cleared visible groups length = %d, want 0: %+v", len(relationGroups), relationGroups)
	}
	var relationUsers []workbenchTypes.ResourceVisibleUser
	if err := sqlStore.db.Where("account_id = ? AND resource_id = ?", "account-1", "server-1").Find(&relationUsers).Error; err != nil {
		t.Fatalf("query cleared visible users error = %v", err)
	}
	if len(relationUsers) != 0 {
		t.Fatalf("cleared visible users length = %d, want 0: %+v", len(relationUsers), relationUsers)
	}
}

func TestSqlStoreDeleteWorkbenchServerResourceCleansVisibility(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	resource := &workbenchTypes.Resource{
		ID:            "server-delete-1",
		AccountID:     "account-1",
		Name:          "待删除资源",
		URL:           "https://delete.example.com",
		Scope:         workbenchTypes.ResourceScopeServer,
		Source:        workbenchTypes.ResourceSourceAdmin,
		Visibility:    workbenchTypes.VisibilityRestricted,
		VisibleGroups: []string{"group-delete"},
		VisibleUsers:  []string{"user-delete"},
		Enabled:       true,
	}
	if err := sqlStore.SaveWorkbenchServerResource(ctx, resource); err != nil {
		t.Fatalf("SaveWorkbenchServerResource() error = %v", err)
	}
	if err := sqlStore.DeleteWorkbenchServerResource(ctx, "account-1", resource.ID); err != nil {
		t.Fatalf("DeleteWorkbenchServerResource() error = %v", err)
	}

	var groups []workbenchTypes.ResourceVisibleGroup
	if err := sqlStore.db.Where("account_id = ? AND resource_id = ?", "account-1", resource.ID).Find(&groups).Error; err != nil {
		t.Fatalf("query visible groups error = %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("visible groups length = %d, want 0: %+v", len(groups), groups)
	}

	var users []workbenchTypes.ResourceVisibleUser
	if err := sqlStore.db.Where("account_id = ? AND resource_id = ?", "account-1", resource.ID).Find(&users).Error; err != nil {
		t.Fatalf("query visible users error = %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("visible users length = %d, want 0: %+v", len(users), users)
	}

	if _, err := sqlStore.GetWorkbenchServerResource(ctx, "account-1", resource.ID); err == nil {
		t.Fatal("GetWorkbenchServerResource() error = nil, want deleted resource not found")
	}
}

func TestSqlStoreWorkbenchAssetRoundTripAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	asset := &workbenchTypes.Asset{
		ID:          "asset-1",
		AccountID:   "account-1",
		OwnerUserID: "user-1",
		SourceURL:   "https://example.com/favicon.ico",
		StoragePath: "/tmp/workbench/assets/asset-1",
		PublicURL:   "/api/workbench/assets/asset-1",
		ContentType: "image/png",
		Size:        123,
		SHA256:      "sha256-value",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := sqlStore.SaveWorkbenchAsset(ctx, asset); err != nil {
		t.Fatalf("SaveWorkbenchAsset() error = %v", err)
	}

	got, err := sqlStore.GetWorkbenchAsset(ctx, "account-1", "asset-1")
	if err != nil {
		t.Fatalf("GetWorkbenchAsset() error = %v", err)
	}
	if got.ID != asset.ID ||
		got.AccountID != asset.AccountID ||
		got.OwnerUserID != asset.OwnerUserID ||
		got.SourceURL != asset.SourceURL ||
		got.StoragePath != asset.StoragePath ||
		got.PublicURL != asset.PublicURL ||
		got.ContentType != asset.ContentType ||
		got.Size != asset.Size ||
		got.SHA256 != asset.SHA256 {
		t.Fatalf("asset round trip mismatch: %+v", got)
	}

	if _, err := sqlStore.GetWorkbenchAsset(ctx, "other-account", "asset-1"); err == nil {
		t.Fatal("GetWorkbenchAsset(other account) error = nil, want not found")
	}

	asset.ContentType = "image/webp"
	asset.Size = 456
	asset.SHA256 = "sha256-updated"
	if err := sqlStore.SaveWorkbenchAsset(ctx, asset); err != nil {
		t.Fatalf("SaveWorkbenchAsset(update) error = %v", err)
	}
	updated, err := sqlStore.GetWorkbenchAsset(ctx, "account-1", "asset-1")
	if err != nil {
		t.Fatalf("GetWorkbenchAsset(updated) error = %v", err)
	}
	if updated.ContentType != "image/webp" || updated.Size != 456 || updated.SHA256 != "sha256-updated" {
		t.Fatalf("updated asset mismatch: %+v", updated)
	}
}

func TestSqlStoreWorkbenchCategoryRoundTripAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	t.Cleanup(cleanup)
	if err != nil {
		t.Fatalf("NewTestStoreFromSQL() error = %v", err)
	}
	sqlStore, ok := store.(*SqlStore)
	if !ok {
		t.Fatalf("store type = %T, want *SqlStore", store)
	}

	category := &workbenchTypes.Category{
		ID:        "category-1",
		AccountID: "account-1",
		Name:      "内部系统",
		Sort:      20,
		CreatedBy: "admin-1",
	}
	if err := sqlStore.SaveWorkbenchCategory(ctx, category); err != nil {
		t.Fatalf("SaveWorkbenchCategory() error = %v", err)
	}

	got, err := sqlStore.GetWorkbenchCategory(ctx, "account-1", "category-1")
	if err != nil {
		t.Fatalf("GetWorkbenchCategory() error = %v", err)
	}
	if got.ID != category.ID || got.AccountID != category.AccountID || got.Name != category.Name || got.Sort != category.Sort || got.CreatedBy != category.CreatedBy {
		t.Fatalf("category round trip mismatch: %+v", got)
	}

	if _, err := sqlStore.GetWorkbenchCategory(ctx, "other-account", "category-1"); err == nil {
		t.Fatal("GetWorkbenchCategory(other account) error = nil, want not found")
	}

	category.Name = "研发系统"
	category.Sort = 10
	if err := sqlStore.SaveWorkbenchCategory(ctx, category); err != nil {
		t.Fatalf("SaveWorkbenchCategory(update) error = %v", err)
	}
	categories, err := sqlStore.GetWorkbenchCategories(ctx, "account-1")
	if err != nil {
		t.Fatalf("GetWorkbenchCategories() error = %v", err)
	}
	if len(categories) != 1 || categories[0].Name != "研发系统" || categories[0].Sort != 10 {
		t.Fatalf("categories mismatch: %+v", categories)
	}

	if err := sqlStore.DeleteWorkbenchCategory(ctx, "account-1", "category-1"); err != nil {
		t.Fatalf("DeleteWorkbenchCategory() error = %v", err)
	}
	if _, err := sqlStore.GetWorkbenchCategory(ctx, "account-1", "category-1"); err == nil {
		t.Fatal("GetWorkbenchCategory(deleted) error = nil, want not found")
	}
}

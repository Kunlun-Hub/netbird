package email

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/permissions/modules"
	"github.com/netbirdio/netbird/management/server/permissions/operations"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
)

const (
	testAccountID = "account-a"
	testUserID    = "user-a"
)

func TestGetSettingsSanitizesPasswordAndMergesDefaultTemplates(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	permissionsMock.EXPECT().
		ValidateUserPermissions(gomock.Any(), testAccountID, testUserID, modules.Settings, operations.Read).
		Return(true, ctx, nil)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID:         testAccountID,
			Enabled:           true,
			Host:              "smtp.example.com",
			Port:              587,
			FromEmail:         "notice@example.com",
			Password:          "secret",
			PasswordEncrypted: "encrypted-secret",
			Templates: map[string]types.EmailTemplate{
				string(types.EmailTemplateInviteUser): {
					Enabled:  true,
					Subject:  "Custom invite",
					BodyText: "hello",
				},
			},
		}, nil)

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	settings, err := manager.GetSettings(ctx, testAccountID, testUserID)
	require.NoError(t, err)

	assert.True(t, settings.PasswordConfigured)
	assert.Empty(t, settings.Password)
	assert.Empty(t, settings.PasswordEncrypted)
	assert.Len(t, settings.Templates, len(DefaultTemplates()))
	assert.Equal(t, "Custom invite", settings.Templates[string(types.EmailTemplateInviteUser)].Subject)
	assert.NotEmpty(t, settings.Templates[string(types.EmailTemplateDevicePendingApproval)].Subject)
}

func TestUpdateSettingsPreservesPasswordWhenPasswordIsOmitted(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	permissionsMock.EXPECT().
		ValidateUserPermissions(gomock.Any(), testAccountID, testUserID, modules.Settings, operations.Update).
		Return(true, ctx, nil)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID:         testAccountID,
			Enabled:           true,
			Host:              "smtp.old.example.com",
			Port:              587,
			FromEmail:         "old@example.com",
			Password:          "old-secret",
			PasswordEncrypted: "encrypted-old-secret",
			Templates:         DefaultTemplates(),
		}, nil)
	storeMock.EXPECT().
		SaveEmailSettings(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, saved *types.EmailSettings) error {
			assert.Equal(t, testAccountID, saved.AccountID)
			assert.Equal(t, "smtp.new.example.com", saved.Host)
			assert.Equal(t, "old-secret", saved.Password)
			assert.Equal(t, "encrypted-old-secret", saved.PasswordEncrypted)
			return nil
		})
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID:         testAccountID,
			Enabled:           true,
			Host:              "smtp.new.example.com",
			Port:              587,
			FromEmail:         "new@example.com",
			Password:          "old-secret",
			PasswordEncrypted: "encrypted-old-secret",
			Templates:         DefaultTemplates(),
		}, nil)

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	settings, err := manager.UpdateSettings(ctx, testAccountID, testUserID, &types.EmailSettings{
		Enabled:   true,
		Host:      "smtp.new.example.com",
		Port:      587,
		FromEmail: "new@example.com",
		Templates: DefaultTemplates(),
	})
	require.NoError(t, err)
	assert.True(t, settings.PasswordConfigured)
	assert.Empty(t, settings.Password)
}

func TestUpdateSettingsClearsPasswordWhenRequested(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	permissionsMock.EXPECT().
		ValidateUserPermissions(gomock.Any(), testAccountID, testUserID, modules.Settings, operations.Update).
		Return(true, ctx, nil)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID:         testAccountID,
			Enabled:           true,
			Host:              "smtp.example.com",
			Port:              587,
			FromEmail:         "notice@example.com",
			Password:          "old-secret",
			PasswordEncrypted: "encrypted-old-secret",
			Templates:         DefaultTemplates(),
		}, nil)
	storeMock.EXPECT().
		SaveEmailSettings(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, saved *types.EmailSettings) error {
			assert.Empty(t, saved.Password)
			assert.Empty(t, saved.PasswordEncrypted)
			return nil
		})
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID: testAccountID,
			Enabled:   true,
			Host:      "smtp.example.com",
			Port:      587,
			FromEmail: "notice@example.com",
			Templates: DefaultTemplates(),
		}, nil)

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	settings, err := manager.UpdateSettings(ctx, testAccountID, testUserID, &types.EmailSettings{
		Enabled:       true,
		Host:          "smtp.example.com",
		Port:          587,
		FromEmail:     "notice@example.com",
		ClearPassword: true,
		Templates:     DefaultTemplates(),
	})
	require.NoError(t, err)
	assert.False(t, settings.PasswordConfigured)
}

func TestUpdateSettingsRejectsInvalidTemplateSyntax(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	permissionsMock.EXPECT().
		ValidateUserPermissions(gomock.Any(), testAccountID, testUserID, modules.Settings, operations.Update).
		Return(true, ctx, nil)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{AccountID: testAccountID, Templates: DefaultTemplates()}, nil)

	templates := DefaultTemplates()
	invalid := templates[string(types.EmailTemplateInviteUser)]
	invalid.Subject = "{{"
	templates[string(types.EmailTemplateInviteUser)] = invalid

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	_, err := manager.UpdateSettings(ctx, testAccountID, testUserID, &types.EmailSettings{
		Enabled:   true,
		Host:      "smtp.example.com",
		Port:      587,
		FromEmail: "notice@example.com",
		Templates: templates,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid email template invite_user subject")
}

func TestPreviewTemplateRendersSampleData(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	permissionsMock.EXPECT().
		ValidateUserPermissions(gomock.Any(), testAccountID, testUserID, modules.Settings, operations.Update).
		Return(true, ctx, nil)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{
			AccountID: testAccountID,
			Templates: map[string]types.EmailTemplate{
				string(types.EmailTemplateInviteUser): {
					Enabled:  true,
					Subject:  "Invite {{.user.email}}",
					BodyHTML: "<p>{{.invite.url}}</p>",
					BodyText: "Go to {{.invite.url}}",
				},
			},
		}, nil)

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	rendered, err := manager.PreviewTemplate(ctx, testAccountID, testUserID, types.EmailTemplateInviteUser, TemplateData{
		"user":   map[string]any{"email": "user@example.com"},
		"invite": map[string]any{"url": "https://dash.example.com/invite?token=abc"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Invite user@example.com", rendered.Subject)
	assert.Equal(t, "<p>https://dash.example.com/invite?token=abc</p>", rendered.BodyHTML)
	assert.Equal(t, "Go to https://dash.example.com/invite?token=abc", rendered.BodyText)
}

func TestNotifyReturnsNilWhenDisabled(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	storeMock := store.NewMockStore(ctrl)
	permissionsMock := permissions.NewMockManager(ctrl)
	storeMock.EXPECT().
		GetEmailSettings(gomock.Any(), store.LockingStrengthNone, testAccountID).
		Return(&types.EmailSettings{AccountID: testAccountID, Enabled: false}, nil)

	manager := NewManager(storeMock, permissionsMock, "https://dash.example.com")
	require.NoError(t, manager.Notify(ctx, testAccountID, types.EmailTemplateInviteUser, nil))
}

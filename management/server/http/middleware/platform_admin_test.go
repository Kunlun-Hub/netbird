package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	nbcontext "github.com/netbirdio/netbird/management/server/context"
	"github.com/netbirdio/netbird/management/server/saas"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/types"
	"github.com/netbirdio/netbird/shared/auth"
)

func TestPlatformAdminMiddleware(t *testing.T) {
	ctx := context.Background()
	s, cleanup, err := store.NewTestStoreFromSQL(ctx, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := s.SaveSaaSPlatformAdmin(ctx, &types.SaaSPlatformAdmin{
		UserID:  "platform-admin",
		Role:    types.SaaSPlatformRoleAdmin,
		Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSaaSPlatformAdmin(ctx, &types.SaaSPlatformAdmin{
		UserID:  "disabled-admin",
		Role:    types.SaaSPlatformRoleAdmin,
		Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}

	middleware := NewPlatformAdminMiddleware(saas.PlatformAuthorizer{Store: s})
	protected := middleware.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name       string
		userID     string
		wantStatus int
	}{
		{name: "platform admin", userID: "platform-admin", wantStatus: http.StatusNoContent},
		{name: "ordinary owner", userID: "ordinary-owner", wantStatus: http.StatusForbidden},
		{name: "disabled admin", userID: "disabled-admin", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/platform/orgs", nil)
			req = nbcontext.SetUserAuthInRequest(req, auth.UserAuth{UserId: tt.userID, AccountId: "account-a"})
			rec := httptest.NewRecorder()

			protected.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

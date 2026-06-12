package server

import (
	"testing"

	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
)

func TestInviteURLUsesPublicOriginFromReverseProxyCallbackURL(t *testing.T) {
	manager := &DefaultAccountManager{
		config: &nbconfig.Config{
			HttpConfig: &nbconfig.HttpServerConfig{
				AuthCallbackURL: "https://cloink.4w.ink/api/reverse-proxy/callback",
			},
		},
	}

	got := manager.inviteURL("nbi_token")
	want := "https://cloink.4w.ink/invite?token=nbi_token"
	if got != want {
		t.Fatalf("inviteURL() = %q, want %q", got, want)
	}
}

func TestDashboardURLUsesPublicOriginFromOAuthCallbackURL(t *testing.T) {
	manager := &DefaultAccountManager{
		config: &nbconfig.Config{
			HttpConfig: &nbconfig.HttpServerConfig{
				AuthCallbackURL: "https://cloink.4w.ink/oauth2/callback",
			},
		},
	}

	got := manager.dashboardURL("/team?status=pending")
	want := "https://cloink.4w.ink/team?status=pending"
	if got != want {
		t.Fatalf("dashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURLPrefersPublicAuthAudienceWhenItIsURL(t *testing.T) {
	manager := &DefaultAccountManager{
		config: &nbconfig.Config{
			HttpConfig: &nbconfig.HttpServerConfig{
				AuthAudience:    "https://dashboard.example.com",
				AuthCallbackURL: "https://management.example.com/api/reverse-proxy/callback",
			},
		},
	}

	got := manager.dashboardURL("/invite")
	want := "https://dashboard.example.com/invite"
	if got != want {
		t.Fatalf("dashboardURL() = %q, want %q", got, want)
	}
}

func TestDashboardURLFallsBackToRelativePathWithoutPublicURL(t *testing.T) {
	manager := &DefaultAccountManager{
		config: &nbconfig.Config{
			HttpConfig: &nbconfig.HttpServerConfig{
				AuthAudience: "netbird-dashboard",
			},
		},
	}

	got := manager.dashboardURL("/invite")
	want := "/invite"
	if got != want {
		t.Fatalf("dashboardURL() = %q, want %q", got, want)
	}
}

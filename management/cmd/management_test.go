package cmd

import (
	"context"
	"os"
	"testing"
)

const (
	exampleConfig = `{
	  "Relay": {
		"Addresses": [
		  "rel://192.168.100.1:8085",
		  "rel://192.168.100.1:8086"
		],
		"CredentialsTTL": "12h0m0s",
		"Secret": "8f7e9d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8"
	  },
	  "HttpConfig": {
		"AuthAudience": "https://stageapp/",
		"AuthIssuer": "https://something.eu.auth0.com/",
		"OIDCConfigEndpoint": "https://something.eu.auth0.com/.well-known/openid-configuration"
	  }
	}`

	saasConfig = `{
	  "HttpConfig": {
		"AuthAudience": "https://stageapp/",
		"AuthIssuer": "https://something.eu.auth0.com/"
	  },
	  "SaaS": {
		"Enabled": true,
		"PublicSignupEnabled": true,
		"RootDomain": "cloink.4w.ink",
		"Payment": {
		  "Provider": "alipay",
		  "Alipay": {
			"NotifyURL": "https://admin.cloink.4w.ink/api/saas/payments/alipay/notify",
			"ReturnURL": "https://admin.cloink.4w.ink/billing/orders"
		  }
		}
	  }
	}`
)

func Test_loadMgmtConfig(t *testing.T) {
	tmpFile, err := createConfig()
	if err != nil {
		t.Fatalf("failed to create config: %s", err)
	}

	cfg, err := LoadMgmtConfig(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to load management config: %s", err)
	}
	if cfg.Relay == nil {
		t.Fatalf("config is nil")
	}
	if len(cfg.Relay.GetAddresses()) == 0 {
		t.Fatalf("relay address is empty")
	}
	if cfg.SaaS.Enabled {
		t.Fatalf("saas should be disabled by default")
	}
	if cfg.SaaS.DefaultPlan != "free" {
		t.Fatalf("unexpected default SaaS plan: %s", cfg.SaaS.DefaultPlan)
	}
}

func TestLoadMgmtConfigSaaSDefaults(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "config.json")
	if err != nil {
		t.Fatalf("failed creating config: %s", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(saasConfig)); err != nil {
		t.Fatalf("failed writing config: %s", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("failed closing config: %s", err)
	}

	cfg, err := LoadMgmtConfig(context.Background(), tmpfile.Name())
	if err != nil {
		t.Fatalf("failed to load management config: %s", err)
	}

	if !cfg.SaaS.Enabled {
		t.Fatalf("saas should be enabled from config")
	}
	if cfg.SaaS.OrganizationDomainSuffix != "cloink.4w.ink" {
		t.Fatalf("unexpected organization domain suffix: %s", cfg.SaaS.OrganizationDomainSuffix)
	}
	if cfg.SaaS.PlatformAdminDomain != "admin.cloink.4w.ink" {
		t.Fatalf("unexpected platform admin domain: %s", cfg.SaaS.PlatformAdminDomain)
	}
	if cfg.SaaS.DefaultUsersLimit != 3 || cfg.SaaS.DefaultPeersLimit != 10 {
		t.Fatalf("unexpected SaaS limits: users=%d peers=%d", cfg.SaaS.DefaultUsersLimit, cfg.SaaS.DefaultPeersLimit)
	}
	if cfg.SaaS.Payment.Alipay.NotifyURL == "" || cfg.SaaS.Payment.Alipay.ReturnURL == "" {
		t.Fatalf("expected alipay callback URLs to load")
	}
}

func createConfig() (string, error) {
	tmpfile, err := os.CreateTemp("", "config.json")
	if err != nil {
		return "", err
	}
	_, err = tmpfile.Write([]byte(exampleConfig))
	if err != nil {
		return "", err
	}

	if err := tmpfile.Close(); err != nil {
		return "", err
	}
	return tmpfile.Name(), nil
}

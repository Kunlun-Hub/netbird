package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/netbirdio/netbird/client/internal/profilemanager"
	"github.com/netbirdio/netbird/util"
)

func TestLogin(t *testing.T) {
	mgmAddr := startTestingServices(t)

	tempDir := t.TempDir()

	currUser, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
		return
	}

	origDefaultProfileDir := profilemanager.DefaultConfigPathDir
	origActiveProfileStatePath := profilemanager.ActiveProfileStatePath
	profilemanager.DefaultConfigPathDir = tempDir
	profilemanager.ActiveProfileStatePath = tempDir + "/active_profile.json"
	sm := profilemanager.ServiceManager{}
	err = sm.SetActiveProfileState(&profilemanager.ActiveProfileState{
		Name:     "default",
		Username: currUser.Username,
	})
	if err != nil {
		t.Fatalf("failed to set active profile state: %v", err)
	}

	t.Cleanup(func() {
		profilemanager.DefaultConfigPathDir = origDefaultProfileDir
		profilemanager.ActiveProfileStatePath = origActiveProfileStatePath
	})

	mgmtURL := fmt.Sprintf("http://%s", mgmAddr)
	rootCmd.SetArgs([]string{
		"login",
		"--log-file",
		util.LogConsole,
		"--setup-key",
		strings.ToUpper("a2c8e62b-38f5-4553-b31e-dd66c696cebb"),
		"--management-url",
		mgmtURL,
	})
	// TODO(hakan): fix this test
	_ = rootCmd.Execute()
}

func TestPrintDeviceApprovalHint(t *testing.T) {
	tests := []struct {
		name             string
		requiresApproval bool
		approvalURL      string
		wantContains     []string
		wantEmpty        bool
	}{
		{
			name:      "does not print when approval is not required",
			wantEmpty: true,
		},
		{
			name:             "prints approval hint and page URL",
			requiresApproval: true,
			approvalURL:      "https://saas.example.com/device-approval?device=test-device",
			wantContains: []string{
				"您的设备需要管理员审批。",
				"待审批提示页: https://saas.example.com/device-approval?device=test-device",
			},
		},
		{
			name:             "prints approval hint without page URL",
			requiresApproval: true,
			wantContains: []string{
				"您的设备需要管理员审批。",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&buf)

			printDeviceApprovalHint(cmd, tt.requiresApproval, tt.approvalURL)

			output := buf.String()
			if tt.wantEmpty && output != "" {
				t.Fatalf("expected no output, got %q", output)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Fatalf("expected output to contain %q, got %q", want, output)
				}
			}
		})
	}
}

func TestSetActiveProfileEmailPreservesWorkbenchTokenState(t *testing.T) {
	tempDir := t.TempDir()
	origDefaultProfileDir := profilemanager.DefaultConfigPathDir
	origDefaultConfigPath := profilemanager.DefaultConfigPath
	origActiveProfileStatePath := profilemanager.ActiveProfileStatePath
	origConfigDirOverride := profilemanager.ConfigDirOverride
	profilemanager.DefaultConfigPathDir = tempDir
	profilemanager.DefaultConfigPath = filepath.Join(tempDir, "default.json")
	profilemanager.ActiveProfileStatePath = filepath.Join(tempDir, "active_profile.json")
	profilemanager.ConfigDirOverride = tempDir
	t.Cleanup(func() {
		profilemanager.DefaultConfigPathDir = origDefaultProfileDir
		profilemanager.DefaultConfigPath = origDefaultConfigPath
		profilemanager.ActiveProfileStatePath = origActiveProfileStatePath
		profilemanager.ConfigDirOverride = origConfigDirOverride
	})

	activePath := filepath.Join(tempDir, "active_profile.txt")
	if err := os.WriteFile(activePath, []byte("default"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", activePath, err)
	}
	statePath := filepath.Join(tempDir, "default.state.json")
	if err := os.WriteFile(statePath, []byte(`{
		"email":"old@example.com",
		"management_api_token":"token-123",
		"token_type":"Bearer",
		"token_expires_at":4102444800
	}`), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", statePath, err)
	}

	pm := profilemanager.NewProfileManager()
	if err := setActiveProfileEmail(pm, "new@example.com"); err != nil {
		t.Fatalf("setActiveProfileEmail() error = %v", err)
	}

	state, err := pm.GetProfileState("default")
	if err != nil {
		t.Fatalf("GetProfileState() error = %v", err)
	}
	if state.Email != "new@example.com" {
		t.Fatalf("Email = %q, want new@example.com", state.Email)
	}
	if state.ManagementAPIToken != "token-123" || state.TokenType != "Bearer" || state.TokenExpiresAt != 4102444800 {
		t.Fatalf("workbench token state was not preserved: %+v", state)
	}
}

package cmd

import (
	"bytes"
	"fmt"
	"os/user"
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

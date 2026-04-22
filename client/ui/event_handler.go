//go:build !(linux && 386)

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"fyne.io/fyne/v2"
	"fyne.io/systray"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/netbirdio/netbird/client/proto"
)

type eventHandler struct {
	client *serviceClient
}

func newEventHandler(client *serviceClient) *eventHandler {
	return &eventHandler{
		client: client,
	}
}

func (h *eventHandler) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.client.mUp.ClickedCh:
			h.handleConnectClick()
		case <-h.client.mDown.ClickedCh:
			h.handleDisconnectClick()
		case <-h.client.mNetworks.ClickedCh:
			h.handleNetworksClick()
		case <-h.client.mAllowSSH.ClickedCh:
			h.handleAllowSSHClick()
		case <-h.client.mAutoConnect.ClickedCh:
			h.handleAutoConnectClick()
		case <-h.client.mEnableRosenpass.ClickedCh:
			h.handleRosenpassClick()
		case <-h.client.mLazyConnEnabled.ClickedCh:
			h.handleLazyConnectionClick()
		case <-h.client.mBlockInbound.ClickedCh:
			h.handleBlockInboundClick()
		case <-h.client.mAdvancedSettings.ClickedCh:
			h.handleAdvancedSettingsClick()
		case <-h.client.mCreateDebugBundle.ClickedCh:
			h.handleCreateDebugBundleClick()
		case <-h.client.mNotifications.ClickedCh:
			h.handleNotificationsClick()
		case <-h.client.mQuit.ClickedCh:
			h.handleQuitClick()
			return
		}
	}
}

func (h *eventHandler) handleConnectClick() {
	h.client.mUp.Disable()

	if h.client.connectCancel != nil {
		h.client.connectCancel()
	}

	connectCtx, connectCancel := context.WithCancel(h.client.ctx)
	h.client.connectCancel = connectCancel

	go func() {
		defer connectCancel()

		if err := h.client.menuUpClick(connectCtx); err != nil {
			st, ok := status.FromError(err)
			if errors.Is(err, context.Canceled) || (ok && st.Code() == codes.Canceled) {
				log.Debugf("connect operation cancelled by user")
			} else {
				h.client.app.SendNotification(fyne.NewNotification("错误", "连接失败"))
				log.Errorf("connect failed: %v", err)
			}
		}

		if err := h.client.updateStatus(); err != nil {
			log.Debugf("failed to update status after connect: %v", err)
		}
	}()
}

func (h *eventHandler) handleDisconnectClick() {
	h.client.mDown.Disable()
	h.client.cancelExitNodeRetry()

	if h.client.connectCancel != nil {
		log.Debugf("cancelling ongoing connect operation")
		h.client.connectCancel()
		h.client.connectCancel = nil
	}

	go func() {
		if err := h.client.menuDownClick(); err != nil {
			st, ok := status.FromError(err)
			if !errors.Is(err, context.Canceled) && !(ok && st.Code() == codes.Canceled) {
				h.client.app.SendNotification(fyne.NewNotification("错误", "断开连接失败"))
				log.Errorf("disconnect failed: %v", err)
			} else {
				log.Debugf("disconnect cancelled or already disconnecting")
			}
		}

		if err := h.client.updateStatus(); err != nil {
			log.Debugf("failed to update status after disconnect: %v", err)
		}
	}()
}

func (h *eventHandler) handleQuitClick() {
	systray.Quit()
}

func (h *eventHandler) handleNetworksClick() {
	h.client.mNetworks.Disable()
	go func() {
		defer h.client.mNetworks.Enable()
		h.runSelfCommand(h.client.ctx, "networks")
	}()
}

func (h *eventHandler) handleAllowSSHClick() {
	h.toggleCheckbox(h.client.mAllowSSH)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mAllowSSH)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新 SSH 设置失败"))
	}
}

func (h *eventHandler) handleAutoConnectClick() {
	h.toggleCheckbox(h.client.mAutoConnect)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mAutoConnect)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新自动连接设置失败"))
	}
}

func (h *eventHandler) handleRosenpassClick() {
	h.toggleCheckbox(h.client.mEnableRosenpass)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mEnableRosenpass)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新量子抗性设置失败"))
	}
}

func (h *eventHandler) handleLazyConnectionClick() {
	h.toggleCheckbox(h.client.mLazyConnEnabled)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mLazyConnEnabled)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新懒连接设置失败"))
	}
}

func (h *eventHandler) handleBlockInboundClick() {
	h.toggleCheckbox(h.client.mBlockInbound)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mBlockInbound)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新阻止入站连接设置失败"))
	}
}

func (h *eventHandler) handleNotificationsClick() {
	h.toggleCheckbox(h.client.mNotifications)
	if err := h.updateConfigWithErr(); err != nil {
		h.toggleCheckbox(h.client.mNotifications)
		log.Errorf("failed to update config: %v", err)
		h.client.app.SendNotification(fyne.NewNotification("错误", "更新通知设置失败"))
	} else if h.client.eventManager != nil {
		h.client.eventManager.SetNotificationsEnabled(h.client.mNotifications.Checked())
	}
}

func (h *eventHandler) handleAdvancedSettingsClick() {
	h.client.mAdvancedSettings.Disable()
	go func() {
		defer h.client.mAdvancedSettings.Enable()
		defer h.client.getSrvConfig()
		h.runSelfCommand(h.client.ctx, "settings")
	}()
}

func (h *eventHandler) handleCreateDebugBundleClick() {
	h.client.mCreateDebugBundle.Disable()
	go func() {
		defer h.client.mCreateDebugBundle.Enable()
		h.runSelfCommand(h.client.ctx, "debug")
	}()
}

func (h *eventHandler) toggleCheckbox(item *systray.MenuItem) {
	if item.Checked() {
		item.Uncheck()
	} else {
		item.Check()
	}
}

func (h *eventHandler) updateConfigWithErr() error {
	if err := h.client.updateConfig(); err != nil {
		return err
	}
	return nil
}

func (h *eventHandler) runSelfCommand(ctx context.Context, command string, args ...string) {
	proc, err := os.Executable()
	if err != nil {
		log.Errorf("error getting executable path: %v", err)
		return
	}

	// Build the full command arguments
	cmdArgs := []string{
		fmt.Sprintf("--%s=true", command),
		fmt.Sprintf("--daemon-addr=%s", h.client.addr),
		"--use-log-file",
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, proc, cmdArgs...)

	// Print command details for debugging
	log.Printf("Running command: %s", cmd.String())
	log.Printf("Executable path: %s", proc)
	log.Printf("Command arguments: %v", cmdArgs)

	if out := h.client.attachOutput(cmd); out != nil {
		defer func() {
			if err := out.Close(); err != nil {
				log.Errorf("error closing log file %s: %v", h.client.logFile, err)
			}
		}()
	}

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			log.Printf("command '%s' failed with exit code %d", cmd.String(), exitErr.ExitCode())
			log.Printf("Command stderr: %s", string(exitErr.Stderr))
		}
		log.Errorf("Command execution error: %v", err)
		return
	}

	log.Printf("command '%s' completed successfully", cmd.String())
}

func (h *eventHandler) logout(ctx context.Context) error {
	client, err := h.client.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf("failed to get service client: %w", err)
	}

	_, err = client.Logout(ctx, &proto.LogoutRequest{})
	if err != nil {
		return fmt.Errorf("logout failed: %w", err)
	}

	h.client.getSrvConfig()

	return nil
}

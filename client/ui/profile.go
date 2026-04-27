//go:build !(linux && 386)

package main

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/client/internal"
	"github.com/netbirdio/netbird/client/internal/profilemanager"
	"github.com/netbirdio/netbird/client/proto"
)

// showProfilesUI creates and displays the Profiles window with a list of existing profiles,
// a button to add new profiles, allows removal, and lets the user switch the active profile.
func (s *serviceClient) showProfilesUI() {

	profiles, err := s.getProfiles()
	if err != nil {
		log.Errorf("get profiles: %v", err)
		return
	}

	var refresh func()
	// List widget for profiles
	list := widget.NewList(
		func() int { return len(profiles) },
		func() fyne.CanvasObject {
			// Each item: Selected indicator, Name, spacer, Select, Logout & Remove buttons
			return container.NewHBox(
				widget.NewLabel(""), // indicator
				widget.NewLabel(""), // profile name
				layout.NewSpacer(),
				widget.NewButton("Select", nil),
				widget.NewButton("Deregister", nil),
				widget.NewButton("Remove", nil),
			)
		},
		func(i widget.ListItemID, item fyne.CanvasObject) {
			// Populate each row
			row := item.(*fyne.Container)
			indicator := row.Objects[0].(*widget.Label)
			nameLabel := row.Objects[1].(*widget.Label)
			selectBtn := row.Objects[3].(*widget.Button)
			logoutBtn := row.Objects[4].(*widget.Button)
			removeBtn := row.Objects[5].(*widget.Button)

			profile := profiles[i]
			// Show a checkmark if selected
			if profile.IsActive {
				indicator.SetText("✓")
			} else {
				indicator.SetText("")
			}
			nameLabel.SetText(profile.Name)

			// Configure Select/Active button
			selectBtn.SetText(func() string {
				if profile.IsActive {
					return "Active"
				}
				return "Select"
			}())
			selectBtn.OnTapped = func() {
				if profile.IsActive {
					return // already active
				}
				// confirm switch
				dialog.ShowConfirm(
					"切换配置",
					fmt.Sprintf("您确定要切换到 '%s' 吗?", profile.Name),
					func(confirm bool) {
						if !confirm {
							return
						}
						// switch
						err = s.switchProfile(profile.Name)
						if err != nil {
							log.Errorf("failed to switch profile: %v", err)
							dialog.ShowError(errors.New("failed to select profile"), s.wProfiles)
							return
						}

						dialog.ShowInformation(
							"配置已切换",
							fmt.Sprintf("配置 '%s' 已成功切换", profile.Name),
							s.wProfiles,
						)

						conn, err := s.getSrvClient(defaultFailTimeout)
						if err != nil {
							log.Errorf("failed to get daemon client: %v", err)
							return
						}

						status, err := conn.Status(s.ctx, &proto.StatusRequest{})
						if err != nil {
							log.Errorf("failed to get status after switching profile: %v", err)
							return
						}

						if status.Status == string(internal.StatusConnected) {
							if err := s.menuDownClick(); err != nil {
								log.Errorf("failed to handle down click after switching profile: %v", err)
								dialog.ShowError(fmt.Errorf("failed to handle down click"), s.wProfiles)
								return
							}
						}
						// update slice flags
						refresh()
					},
					s.wProfiles,
				)
			}

			logoutBtn.Show()
			logoutBtn.SetText("Deregister")
			logoutBtn.OnTapped = func() {
				s.handleProfileLogout(profile.Name, refresh)
			}

			// Remove profile
			removeBtn.SetText("删除")
			removeBtn.OnTapped = func() {
				dialog.ShowConfirm(
					"删除配置",
					fmt.Sprintf("您确定要删除 '%s' 吗?", profile.Name),
					func(confirm bool) {
						if !confirm {
							return
						}

						err = s.removeProfile(profile.Name)
						if err != nil {
							log.Errorf("failed to remove profile: %v", err)
							dialog.ShowError(fmt.Errorf("failed to remove profile"), s.wProfiles)
							return
						}
						dialog.ShowInformation(
							"配置已删除",
							fmt.Sprintf("配置 '%s' 已成功删除", profile.Name),
							s.wProfiles,
						)
						// update slice
						refresh()
					},
					s.wProfiles,
				)
			}
		},
	)

	refresh = func() {
		newProfiles, err := s.getProfiles()
		if err != nil {
			dialog.ShowError(err, s.wProfiles)
			return
		}
		profiles = newProfiles // update the slice
		list.Refresh()         // tell Fyne to re-call length/update on every visible row
	}

	// Button to add a new profile
	newBtn := widget.NewButton("New Profile", func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("输入配置名称")

		formItems := []*widget.FormItem{{Text: "名称:", Widget: nameEntry}}
		dlg := dialog.NewForm(
			"新建配置",
			"创建",
			"取消",
			formItems,
			func(confirm bool) {
				if !confirm {
					return
				}
				name := nameEntry.Text
				if name == "" {
					dialog.ShowError(errors.New("配置名称不能为空"), s.wProfiles)
					return
				}

				// add profile
				err = s.addProfile(name)
				if err != nil {
					log.Errorf("failed to create profile: %v", err)
					dialog.ShowError(fmt.Errorf("failed to create profile"), s.wProfiles)
					return
				}
				dialog.ShowInformation(
					"配置已创建",
					fmt.Sprintf("配置 '%s' 已成功创建", name),
					s.wProfiles,
				)
				// update slice
				refresh()
			},
			s.wProfiles,
		)
		// make dialog wider
		dlg.Resize(fyne.NewSize(350, 150))
		dlg.Show()
	})

	// Assemble window content
	content := container.NewBorder(nil, newBtn, nil, nil, list)
	s.wProfiles = s.app.NewWindow("Cloink Profiles")
	s.wProfiles.SetContent(content)
	s.wProfiles.Resize(fyne.NewSize(400, 300))
	s.wProfiles.SetOnClosed(s.cancel)

	s.wProfiles.Show()
}

func (s *serviceClient) addProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	_, err = conn.AddProfile(s.ctx, &proto.AddProfileRequest{
		ProfileName: profileName,
		Username:    currUser.Username,
	})

	if err != nil {
		return fmt.Errorf("add profile: %w", err)
	}

	return nil
}

func (s *serviceClient) switchProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	if _, err := conn.SwitchProfile(s.ctx, &proto.SwitchProfileRequest{
		ProfileName: &profileName,
		Username:    &currUser.Username,
	}); err != nil {
		return fmt.Errorf("switch profile failed: %w", err)
	}

	err = s.profileManager.SwitchProfile(profileName)
	if err != nil {
		return fmt.Errorf("switch profile: %w", err)
	}

	return nil
}

func (s *serviceClient) removeProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	_, err = conn.RemoveProfile(s.ctx, &proto.RemoveProfileRequest{
		ProfileName: profileName,
		Username:    currUser.Username,
	})
	if err != nil {
		return fmt.Errorf("remove profile: %w", err)
	}

	return nil
}

type Profile struct {
	Name     string
	IsActive bool
}

func (s *serviceClient) getProfiles() ([]Profile, error) {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return nil, fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}
	profilesResp, err := conn.ListProfiles(s.ctx, &proto.ListProfilesRequest{
		Username: currUser.Username,
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	var profiles []Profile

	for _, profile := range profilesResp.Profiles {
		profiles = append(profiles, Profile{
			Name:     profile.Name,
			IsActive: profile.IsActive,
		})
	}

	return profiles, nil
}

func (s *serviceClient) handleProfileLogout(profileName string, refreshCallback func()) {
	dialog.ShowConfirm(
		"注销",
		fmt.Sprintf("您确定要从 '%s' 注销吗?", profileName),
		func(confirm bool) {
			if !confirm {
				return
			}

			conn, err := s.getSrvClient(defaultFailTimeout)
			if err != nil {
				log.Errorf("failed to get service client: %v", err)
				dialog.ShowError(fmt.Errorf("failed to connect to service"), s.wProfiles)
				return
			}

			currUser, err := user.Current()
			if err != nil {
				log.Errorf("failed to get current user: %v", err)
				dialog.ShowError(fmt.Errorf("failed to get current user"), s.wProfiles)
				return
			}

			username := currUser.Username
			_, err = conn.Logout(s.ctx, &proto.LogoutRequest{
				ProfileName: &profileName,
				Username:    &username,
			})
			if err != nil {
				log.Errorf("logout failed: %v", err)
				dialog.ShowError(fmt.Errorf("deregister failed"), s.wProfiles)
				return
			}

			dialog.ShowInformation(
				"已注销",
				fmt.Sprintf("已成功从 '%s' 注销", profileName),
				s.wProfiles,
			)

			refreshCallback()
		},
		s.wProfiles,
	)
}

type subItem struct {
	*systray.MenuItem
	ctx    context.Context
	cancel context.CancelFunc
}

type profileMenu struct {
	mu                    sync.Mutex
	ctx                   context.Context
	profileManager        *profilemanager.ProfileManager
	profileMenuItem       *systray.MenuItem
	emailMenuItem         *systray.MenuItem
	getSrvClientCallback  func(timeout time.Duration) (proto.DaemonServiceClient, error)
}

type newProfileMenuArgs struct {
	ctx                  context.Context
	profileManager       *profilemanager.ProfileManager
	profileMenuItem      *systray.MenuItem
	emailMenuItem        *systray.MenuItem
	getSrvClientCallback func(timeout time.Duration) (proto.DaemonServiceClient, error)
}

func newProfileMenu(args newProfileMenuArgs) *profileMenu {
	p := profileMenu{
		ctx:                  args.ctx,
		profileManager:       args.profileManager,
		profileMenuItem:      args.profileMenuItem,
		emailMenuItem:        args.emailMenuItem,
		getSrvClientCallback: args.getSrvClientCallback,
	}

	p.profileMenuItem.Disable()
	p.emailMenuItem.Disable()
	p.emailMenuItem.Hide()
	p.refresh()
	go p.updateMenu()

	return &p
}

func (p *profileMenu) getProfiles() ([]Profile, error) {
	conn, err := p.getSrvClientCallback(defaultFailTimeout)
	if err != nil {
		return nil, fmt.Errorf(getClientFMT, err)
	}
	currUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	profilesResp, err := conn.ListProfiles(p.ctx, &proto.ListProfilesRequest{
		Username: currUser.Username,
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	var profiles []Profile

	for _, profile := range profilesResp.Profiles {
		profiles = append(profiles, Profile{
			Name:     profile.Name,
			IsActive: profile.IsActive,
		})
	}

	return profiles, nil
}

func (p *profileMenu) refresh() {
	p.mu.Lock()
	defer p.mu.Unlock()

	currUser, err := user.Current()
	if err != nil {
		log.Errorf("failed to get current user: %v", err)
		return
	}

	conn, err := p.getSrvClientCallback(defaultFailTimeout)
	if err != nil {
		log.Errorf("failed to get daemon client: %v", err)
		return
	}

	activeProf, err := conn.GetActiveProfile(p.ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		log.Errorf("failed to get active profile: %v", err)
		return
	}

	if activeProf.ProfileName == "default" || activeProf.Username == currUser.Username {
		activeProfState, err := p.profileManager.GetProfileState(activeProf.ProfileName)
		if err != nil {
			log.Warnf("failed to get active profile state: %v", err)
			p.emailMenuItem.Hide()
		} else if activeProfState.Email != "" {
			p.emailMenuItem.SetTitle(fmt.Sprintf("(%s)", activeProfState.Email))
			p.emailMenuItem.Show()
		}
	} else {
		p.emailMenuItem.Hide()
	}

	if activeProf.ProfileName == "default" || activeProf.Username == currUser.Username {
		p.profileMenuItem.SetTitle(activeProf.ProfileName)
	} else {
		p.profileMenuItem.SetTitle(fmt.Sprintf("Profile: %s (User: %s)", activeProf.ProfileName, activeProf.Username))
		p.emailMenuItem.Hide()
	}
}

// setEnabled enables or disables the profile menu based on the provided state
func (p *profileMenu) setEnabled(enabled bool) {
	if p.profileMenuItem == nil || p.emailMenuItem == nil {
		return
	}
	if enabled {
		p.profileMenuItem.Show()
		p.refresh()
		return
	}
	p.profileMenuItem.Hide()
	p.emailMenuItem.Hide()
}

func (p *profileMenu) updateMenu() {
	// check every second
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.refresh()
		case <-p.ctx.Done():
			return // context cancelled

		}
	}
}

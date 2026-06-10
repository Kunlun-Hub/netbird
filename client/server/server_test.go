package server

import (
	"context"
	"net"
	"net/url"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/netbirdio/netbird/management/server/integrations/integrated_validator/validator"

	"github.com/netbirdio/netbird/management/internals/controllers/network_map/controller"
	"github.com/netbirdio/netbird/management/internals/controllers/network_map/update_channel"
	"github.com/netbirdio/netbird/management/internals/modules/peers"
	"github.com/netbirdio/netbird/management/internals/modules/peers/ephemeral/manager"
	nbgrpc "github.com/netbirdio/netbird/management/internals/shared/grpc"
	"github.com/netbirdio/netbird/management/server/job"

	"github.com/netbirdio/netbird/management/internals/server/config"
	"github.com/netbirdio/netbird/management/server/groups"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/netbirdio/netbird/client/internal"
	"github.com/netbirdio/netbird/client/internal/peer"
	"github.com/netbirdio/netbird/client/internal/profilemanager"
	daemonProto "github.com/netbirdio/netbird/client/proto"
	"github.com/netbirdio/netbird/client/system"
	"github.com/netbirdio/netbird/management/server"
	"github.com/netbirdio/netbird/management/server/activity"
	nbcache "github.com/netbirdio/netbird/management/server/cache"
	"github.com/netbirdio/netbird/management/server/integrations/port_forwarding"
	"github.com/netbirdio/netbird/management/server/permissions"
	"github.com/netbirdio/netbird/management/server/settings"
	"github.com/netbirdio/netbird/management/server/store"
	"github.com/netbirdio/netbird/management/server/telemetry"
	mgmtProto "github.com/netbirdio/netbird/shared/management/proto"
	"github.com/netbirdio/netbird/shared/signal/proto"
	signalServer "github.com/netbirdio/netbird/signal/server"
)

var (
	kaep = keepalive.EnforcementPolicy{
		MinTime:             15 * time.Second,
		PermitWithoutStream: true,
	}

	kasp = keepalive.ServerParameters{
		MaxConnectionIdle:     15 * time.Second,
		MaxConnectionAgeGrace: 5 * time.Second,
		Time:                  5 * time.Second,
		Timeout:               2 * time.Second,
	}
)

// TestConnectWithRetryRunsStopsOnPermanentLoginError checks that permanent login errors are not retried by the
// outer supervisor.
func TestConnectWithRetryRunsStopsOnPermanentLoginError(t *testing.T) {
	// start the signal server
	_, signalAddr, err := startSignal(t)
	if err != nil {
		t.Fatalf("failed to start signal server: %v", err)
	}

	counter := 0
	// start the management server
	_, mgmtAddr, err := startManagement(t, signalAddr, &counter)
	if err != nil {
		t.Fatalf("failed to start management server: %v", err)
	}

	ctx := internal.CtxInitState(context.Background())

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(30*time.Second))
	defer cancel()
	// create new server
	ic := profilemanager.ConfigInput{
		ManagementURL: "http://" + mgmtAddr,
		ConfigPath:    t.TempDir() + "/test-profile.json",
	}

	config, err := profilemanager.UpdateOrCreateConfig(ic)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	currUser, err := user.Current()
	require.NoError(t, err)

	pm := profilemanager.ServiceManager{}
	err = pm.SetActiveProfileState(&profilemanager.ActiveProfileState{
		Name:     "test-profile",
		Username: currUser.Username,
	})
	if err != nil {
		t.Fatalf("failed to set active profile state: %v", err)
	}

	s := New(ctx, "debug", "", false, false, false, false)

	s.config = config

	s.statusRecorder = peer.NewRecorder(config.ManagementURL.String())
	t.Setenv(retryInitialIntervalVar, "1s")
	t.Setenv(maxRetryIntervalVar, "2s")
	t.Setenv(maxRetryTimeVar, "5s")
	t.Setenv(retryMultiplierVar, "1")

	s.connectWithRetryRuns(ctx, config, s.statusRecorder, nil, nil)
	if counter != 1 {
		t.Fatalf("expected one login attempt for permanent error, got %d", counter)
	}
}

func TestLoginResponseWithApproval(t *testing.T) {
	adminURL, err := url.Parse("https://saas.example.com:12580/admin")
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), system.DeviceNameCtxKey, "custom-device")

	resp, err := loginResponseWithApproval(ctx, &mgmtProto.LoginResponse{
		PeerConfig: &mgmtProto.PeerConfig{
			RequiresApproval: true,
		},
	}, &profilemanager.Config{AdminURL: adminURL}, "user@example.com")
	require.NoError(t, err)

	require.True(t, resp.GetRequiresApproval())
	require.NotEmpty(t, resp.GetDeviceApprovalURL())

	approvalURL, err := url.Parse(resp.GetDeviceApprovalURL())
	require.NoError(t, err)
	require.Equal(t, "https", approvalURL.Scheme)
	require.Equal(t, "saas.example.com:12580", approvalURL.Host)
	require.Equal(t, "/device-approval", approvalURL.Path)
	require.Equal(t, "user@example.com", approvalURL.Query().Get("user"))
	require.Equal(t, "custom-device", approvalURL.Query().Get("device"))
}

func TestLoginResponseWithoutApprovalOmitsApprovalURL(t *testing.T) {
	adminURL, err := url.Parse("https://saas.example.com:12580")
	require.NoError(t, err)

	resp, err := loginResponseWithApproval(context.Background(), &mgmtProto.LoginResponse{
		PeerConfig: &mgmtProto.PeerConfig{
			RequiresApproval: false,
		},
	}, &profilemanager.Config{AdminURL: adminURL}, "user@example.com")
	require.NoError(t, err)

	require.False(t, resp.GetRequiresApproval())
	require.Empty(t, resp.GetDeviceApprovalURL())
}

func TestDeviceApprovalURLClearsAdminURLPathQueryAndFragment(t *testing.T) {
	adminURL, err := url.Parse("https://saas.example.com:12580/admin?old=true#section")
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), system.DeviceNameCtxKey, "approval-device")

	actual := deviceApprovalURL(ctx, &profilemanager.Config{AdminURL: adminURL}, "user@example.com")
	approvalURL, err := url.Parse(actual)
	require.NoError(t, err)

	require.Equal(t, "https", approvalURL.Scheme)
	require.Equal(t, "saas.example.com:12580", approvalURL.Host)
	require.Equal(t, "/device-approval", approvalURL.Path)
	require.Empty(t, approvalURL.Fragment)
	require.Equal(t, "user@example.com", approvalURL.Query().Get("user"))
	require.Equal(t, "approval-device", approvalURL.Query().Get("device"))
	require.Empty(t, approvalURL.Query().Get("old"))
}

func TestWaitSSOLoginResponseWithApproval(t *testing.T) {
	adminURL, err := url.Parse("https://saas.example.com:12580")
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), system.DeviceNameCtxKey, "sso-device")

	resp := waitSSOLoginResponseWithApproval(ctx, &mgmtProto.LoginResponse{
		PeerConfig: &mgmtProto.PeerConfig{
			RequiresApproval: true,
		},
	}, &profilemanager.Config{AdminURL: adminURL}, "user@example.com")

	require.Equal(t, "user@example.com", resp.GetEmail())
	require.True(t, resp.GetRequiresApproval())
	require.NotEmpty(t, resp.GetDeviceApprovalURL())

	approvalURL, err := url.Parse(resp.GetDeviceApprovalURL())
	require.NoError(t, err)
	require.Equal(t, "/device-approval", approvalURL.Path)
	require.Equal(t, "user@example.com", approvalURL.Query().Get("user"))
	require.Equal(t, "sso-device", approvalURL.Query().Get("device"))
}

func TestWaitSSOLoginResponseWithoutApprovalOmitsApprovalURL(t *testing.T) {
	adminURL, err := url.Parse("https://saas.example.com:12580")
	require.NoError(t, err)

	resp := waitSSOLoginResponseWithApproval(context.Background(), &mgmtProto.LoginResponse{
		PeerConfig: &mgmtProto.PeerConfig{
			RequiresApproval: false,
		},
	}, &profilemanager.Config{AdminURL: adminURL}, "user@example.com")

	require.Equal(t, "user@example.com", resp.GetEmail())
	require.False(t, resp.GetRequiresApproval())
	require.Empty(t, resp.GetDeviceApprovalURL())
}

func TestServer_Up(t *testing.T) {
	tempDir := t.TempDir()
	origDefaultProfileDir := profilemanager.DefaultConfigPathDir
	origDefaultConfigPath := profilemanager.DefaultConfigPath
	profilemanager.ConfigDirOverride = tempDir
	origActiveProfileStatePath := profilemanager.ActiveProfileStatePath
	profilemanager.DefaultConfigPathDir = tempDir
	profilemanager.ActiveProfileStatePath = tempDir + "/active_profile.json"
	profilemanager.DefaultConfigPath = filepath.Join(tempDir, "default.json")
	t.Cleanup(func() {
		profilemanager.DefaultConfigPathDir = origDefaultProfileDir
		profilemanager.ActiveProfileStatePath = origActiveProfileStatePath
		profilemanager.DefaultConfigPath = origDefaultConfigPath
		profilemanager.ConfigDirOverride = ""
	})

	ctx := internal.CtxInitState(context.Background())

	currUser, err := user.Current()
	require.NoError(t, err)

	profName := "default"

	u, err := url.Parse("http://non-existent-url-for-testing.invalid:12345")
	require.NoError(t, err)

	ic := profilemanager.ConfigInput{
		ConfigPath:    filepath.Join(tempDir, profName+".json"),
		ManagementURL: u.String(),
	}

	_, err = profilemanager.UpdateOrCreateConfig(ic)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	pm := profilemanager.ServiceManager{}
	err = pm.SetActiveProfileState(&profilemanager.ActiveProfileState{
		Name:     profName,
		Username: currUser.Username,
	})
	if err != nil {
		t.Fatalf("failed to set active profile state: %v", err)
	}

	s := New(ctx, "console", "", false, false, false, false)
	err = s.Start()
	require.NoError(t, err)

	upCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	upReq := &daemonProto.UpRequest{
		ProfileName: &profName,
		Username:    &currUser.Username,
	}
	_, err = s.Up(upCtx, upReq)
	log.Errorf("error from Up: %v", err)

	assert.Contains(t, err.Error(), "context deadline exceeded")
}

type mockSubscribeEventsServer struct {
	ctx        context.Context
	sentEvents []*daemonProto.SystemEvent
	grpc.ServerStream
}

func (m *mockSubscribeEventsServer) Send(event *daemonProto.SystemEvent) error {
	m.sentEvents = append(m.sentEvents, event)
	return nil
}

func (m *mockSubscribeEventsServer) Context() context.Context {
	return m.ctx
}

func TestServer_GetEvents(t *testing.T) {
	ctx := internal.CtxInitState(context.Background())
	s := New(ctx, "console", "", false, false, false, false)

	s.statusRecorder.PublishEvent(
		daemonProto.SystemEvent_INFO,
		daemonProto.SystemEvent_AUTHENTICATION,
		"authentication completed",
		"身份认证完成",
		map[string]string{"source": "test"},
	)

	resp, err := s.GetEvents(ctx, &daemonProto.GetEventsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetEvents(), 1)

	event := resp.GetEvents()[0]
	assert.Equal(t, daemonProto.SystemEvent_INFO, event.GetSeverity())
	assert.Equal(t, daemonProto.SystemEvent_AUTHENTICATION, event.GetCategory())
	assert.Equal(t, "authentication completed", event.GetMessage())
	assert.Equal(t, "身份认证完成", event.GetUserMessage())
	assert.Equal(t, "test", event.GetMetadata()["source"])
}

func TestServer_SubcribeEvents(t *testing.T) {
	tempDir := t.TempDir()
	origDefaultProfileDir := profilemanager.DefaultConfigPathDir
	origDefaultConfigPath := profilemanager.DefaultConfigPath
	profilemanager.ConfigDirOverride = tempDir
	origActiveProfileStatePath := profilemanager.ActiveProfileStatePath
	profilemanager.DefaultConfigPathDir = tempDir
	profilemanager.ActiveProfileStatePath = tempDir + "/active_profile.json"
	profilemanager.DefaultConfigPath = filepath.Join(tempDir, "default.json")
	t.Cleanup(func() {
		profilemanager.DefaultConfigPathDir = origDefaultProfileDir
		profilemanager.ActiveProfileStatePath = origActiveProfileStatePath
		profilemanager.DefaultConfigPath = origDefaultConfigPath
		profilemanager.ConfigDirOverride = ""
	})

	ctx := internal.CtxInitState(context.Background())
	ic := profilemanager.ConfigInput{
		ConfigPath: tempDir + "/default.json",
	}

	_, err := profilemanager.UpdateOrCreateConfig(ic)
	if err != nil {
		t.Fatalf("failed to create config: %v", err)
	}

	currUser, err := user.Current()
	require.NoError(t, err)

	pm := profilemanager.ServiceManager{}
	err = pm.SetActiveProfileState(&profilemanager.ActiveProfileState{
		Name:     "default",
		Username: currUser.Username,
	})
	if err != nil {
		t.Fatalf("failed to set active profile state: %v", err)
	}

	s := New(ctx, "console", "", false, false, false, false)

	err = s.Start()
	require.NoError(t, err)

	u, err := url.Parse("http://non-existent-url-for-testing.invalid:12345")
	require.NoError(t, err)
	s.config = &profilemanager.Config{
		ManagementURL: u,
	}

	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	upReq := &daemonProto.SubscribeRequest{}
	mockServer := &mockSubscribeEventsServer{
		ctx:          ctx,
		sentEvents:   make([]*daemonProto.SystemEvent, 0),
		ServerStream: nil,
	}
	err = s.SubscribeEvents(upReq, mockServer)

	assert.NoError(t, err)
}

type mockServer struct {
	mgmtProto.ManagementServiceServer
	counter *int
}

func (m *mockServer) Login(ctx context.Context, req *mgmtProto.EncryptedMessage) (*mgmtProto.EncryptedMessage, error) {
	*m.counter++
	return m.ManagementServiceServer.Login(ctx, req)
}

func startManagement(t *testing.T, signalAddr string, counter *int) (*grpc.Server, string, error) {
	t.Helper()
	dataDir := t.TempDir()

	config := &config.Config{
		Stuns:      []*config.Host{},
		TURNConfig: &config.TURNConfig{},
		Signal: &config.Host{
			Proto: "http",
			URI:   signalAddr,
		},
		Datadir:    dataDir,
		HttpConfig: nil,
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", err
	}
	s := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))
	store, cleanUp, err := store.NewTestStoreFromSQL(context.Background(), "", config.Datadir)
	if err != nil {
		return nil, "", err
	}
	t.Cleanup(cleanUp)

	eventStore := &activity.InMemoryEventStore{}
	if err != nil {
		return nil, "", err
	}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	permissionsManagerMock := permissions.NewMockManager(ctrl)
	peersManager := peers.NewManager(store, permissionsManagerMock)
	settingsManagerMock := settings.NewMockManager(ctrl)

	jobManager := job.NewJobManager(nil, store, peersManager)

	cacheStore, err := nbcache.NewStore(context.Background(), 100*time.Millisecond, 300*time.Millisecond, 100)
	if err != nil {
		return nil, "", err
	}

	ia, _ := validator.NewIntegratedValidator(context.Background(), peersManager, settingsManagerMock, eventStore, cacheStore)

	metrics, err := telemetry.NewDefaultAppMetrics(context.Background())
	require.NoError(t, err)

	settingsMockManager := settings.NewMockManager(ctrl)
	groupsManager := groups.NewManagerMock()

	requestBuffer := server.NewAccountRequestBuffer(context.Background(), store)
	peersUpdateManager := update_channel.NewPeersUpdateManager(metrics)
	networkMapController := controller.NewController(context.Background(), store, metrics, peersUpdateManager, requestBuffer, server.MockIntegratedValidator{}, settingsMockManager, "netbird.selfhosted", port_forwarding.NewControllerMock(), manager.NewEphemeralManager(store, peersManager), config)
	accountManager, err := server.BuildManager(context.Background(), config, store, networkMapController, jobManager, nil, "", eventStore, nil, false, ia, metrics, port_forwarding.NewControllerMock(), settingsMockManager, permissionsManagerMock, false, cacheStore)
	if err != nil {
		return nil, "", err
	}

	secretsManager, err := nbgrpc.NewTimeBasedAuthSecretsManager(peersUpdateManager, config.TURNConfig, config.Relay, settingsMockManager, groupsManager)
	if err != nil {
		return nil, "", err
	}
	mgmtServer, err := nbgrpc.NewServer(config, accountManager, settingsMockManager, jobManager, secretsManager, nil, nil, &server.MockIntegratedValidator{}, networkMapController, nil, nil)
	if err != nil {
		return nil, "", err
	}
	mock := &mockServer{
		ManagementServiceServer: mgmtServer,
		counter:                 counter,
	}
	mgmtProto.RegisterManagementServiceServer(s, mock)
	go func() {
		if err = s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	return s, lis.Addr().String(), nil
}

func startSignal(t *testing.T) (*grpc.Server, string, error) {
	t.Helper()

	s := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	srv, err := signalServer.NewServer(context.Background(), otel.Meter(""))
	require.NoError(t, err)
	proto.RegisterSignalExchangeServer(s, srv)

	go func() {
		if err = s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	return s, lis.Addr().String(), nil
}

package cmd

import (
	"reflect"
	"testing"

	mgmtServer "github.com/netbirdio/netbird/management/internals/server"
	nbconfig "github.com/netbirdio/netbird/management/internals/server/config"
)

func TestNewServerStoresBaseServerInContainer(t *testing.T) {
	srv := newServer(&mgmtServer.Config{NbConfig: &nbconfig.Config{}})
	baseServer, ok := srv.(*mgmtServer.BaseServer)
	if !ok {
		t.Fatalf("expected default combined management server to be *BaseServer, got %T", srv)
	}

	container, ok := srv.GetContainer(mgmtServer.ContainerKeyBaseServer)
	if !ok {
		t.Fatalf("expected base server container entry")
	}
	if container != baseServer {
		t.Fatalf("expected base server container entry to reference default server")
	}
}

func TestSetupServerHooksRegistersWithDirectBaseServer(t *testing.T) {
	baseServer := mgmtServer.NewServer(&mgmtServer.Config{NbConfig: &nbconfig.Config{}})
	servers := &serverInstances{mgmtSrv: baseServer}

	setupServerHooks(servers, &CombinedConfig{})

	if hooks := afterInitHookCount(baseServer); hooks != 1 {
		t.Fatalf("expected one combined server hook, got %d", hooks)
	}
}

func afterInitHookCount(s *mgmtServer.BaseServer) int {
	return reflect.ValueOf(s).Elem().FieldByName("afterInit").Len()
}

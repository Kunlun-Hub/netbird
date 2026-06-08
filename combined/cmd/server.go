package cmd

import (
	mgmtServer "github.com/netbirdio/netbird/management/internals/server"
)

var newServer = func(cfg *mgmtServer.Config) mgmtServer.Server {
	srv := mgmtServer.NewServer(cfg)
	srv.SetContainer(mgmtServer.ContainerKeyBaseServer, srv)
	return srv
}

func SetNewServer(fn func(*mgmtServer.Config) mgmtServer.Server) {
	newServer = fn
}

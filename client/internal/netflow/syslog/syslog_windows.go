//go:build windows
// +build windows

package syslog

import "github.com/netbirdio/netbird/client/internal/netflow/types"

type Sender struct{}

func NewSender(protocol, server, tag, facility string) *Sender {
	return &Sender{}
}

func (s *Sender) Enable() error {
	return nil
}

func (s *Sender) Disable() {}

func (s *Sender) Send(event *types.Event) error {
	return nil
}

func (s *Sender) Close() {}

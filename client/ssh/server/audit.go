package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	log "github.com/sirupsen/logrus"
)

const (
	SSHActivitySessionStart  = "peer.ssh.session.start"
	SSHActivitySessionEnd    = "peer.ssh.session.end"
	SSHActivitySessionDenied = "peer.ssh.session.denied"
	SSHActivityAuthFailed    = "peer.ssh.auth.failed"
	SSHActivityPolicyDenied  = "peer.ssh.policy.denied"

	SSHResultAllowed = "allowed"
	SSHResultDenied  = "denied"
	SSHResultFailed  = "failed"

	SSHAccessMethodNetBird = "netbird-ssh"
	SSHAccessMethodWeb     = "web-ssh"

	SSHSessionTypeShell       = "shell"
	SSHSessionTypeExec        = "exec"
	SSHSessionTypeSFTP        = "sftp"
	SSHSessionTypePortForward = "port-forward"
)

const sshAuditReportTimeout = 5 * time.Second

type SSHAuditEvent struct {
	SessionID            string
	ActivityCode         string
	ActorUserID          string
	ActorUserEmail       string
	SourcePeerIP         string
	DestinationLocalUser string
	AccessMethod         string
	SessionType          string
	Result               string
	Reason               string
	ClientVersion        string
	StartedAt            time.Time
	EndedAt              time.Time
	Duration             time.Duration
}

type SSHAuditReporter interface {
	ReportSSHSessionEvent(ctx context.Context, event SSHAuditEvent) error
}

func (s *Server) reportSSHEvent(ctx context.Context, event SSHAuditEvent) {
	s.mu.RLock()
	reporter := s.auditReporter
	s.mu.RUnlock()
	if reporter == nil {
		return
	}

	if event.SessionID == "" {
		event.SessionID = fmt.Sprintf("%s@%d", event.DestinationLocalUser, time.Now().UnixNano())
	}
	if event.SourcePeerIP == "" {
		event.SourcePeerIP = peerIPFromAddr(remoteAddrFromContext(ctx))
	}
	if event.ClientVersion == "" {
		event.ClientVersion = clientVersionFromContext(ctx)
	}
	if event.AccessMethod == "" {
		event.AccessMethod = accessMethodFromClientVersion(event.ClientVersion)
	}

	go func() {
		reportCtx, cancel := context.WithTimeout(context.Background(), sshAuditReportTimeout)
		defer cancel()

		if err := reporter.ReportSSHSessionEvent(reportCtx, event); err != nil {
			log.WithError(err).Debug("failed to report SSH audit event")
		}
	}()
}

func (s *Server) jwtUserForContext(ctx ssh.Context) string {
	if ctx == nil || ctx.RemoteAddr() == nil {
		return ""
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if state, exists := s.connections[connKey(ctx.RemoteAddr().String())]; exists {
		return state.jwtUsername
	}
	return s.pendingAuthJWT[newAuthKey(ctx.User(), ctx.RemoteAddr())]
}

func auditEventFromContext(ctx ssh.Context, activityCode, sessionType, result, reason string) SSHAuditEvent {
	return SSHAuditEvent{
		SessionID:            sessionIDFromContext(ctx),
		ActivityCode:         activityCode,
		SourcePeerIP:         peerIPFromAddr(remoteAddrFromContext(ctx)),
		DestinationLocalUser: userFromContext(ctx),
		SessionType:          sessionType,
		Result:               result,
		Reason:               reason,
		ClientVersion:        clientVersionFromContext(ctx),
	}
}

func sessionIDFromContext(ctx ssh.Context) string {
	if ctx == nil {
		return ""
	}
	if id := ctx.Value(ssh.ContextKeySessionID); id != nil {
		return fmt.Sprint(id)
	}
	return ""
}

func userFromContext(ctx ssh.Context) string {
	if ctx == nil {
		return ""
	}
	return ctx.User()
}

func remoteAddrFromContext(ctx context.Context) net.Addr {
	if ctx == nil {
		return nil
	}
	if sshCtx, ok := ctx.(ssh.Context); ok {
		return sshCtx.RemoteAddr()
	}
	if addr, ok := ctx.Value(ssh.ContextKeyRemoteAddr).(net.Addr); ok {
		return addr
	}
	return nil
}

func clientVersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sshCtx, ok := ctx.(ssh.Context); ok {
		return sshCtx.ClientVersion()
	}
	if version, ok := ctx.Value(ssh.ContextKeyClientVersion).(string); ok {
		return version
	}
	return ""
}

func peerIPFromAddr(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}

func accessMethodFromClientVersion(clientVersion string) string {
	if strings.Contains(strings.ToLower(clientVersion), "webssh") ||
		strings.Contains(strings.ToLower(clientVersion), "web-ssh") {
		return SSHAccessMethodWeb
	}
	return SSHAccessMethodNetBird
}

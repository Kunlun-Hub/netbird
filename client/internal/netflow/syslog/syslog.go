package syslog

import (
	"encoding/json"
	"fmt"
	"log/syslog"
	"sync"

	"github.com/netbirdio/netbird/client/internal/netflow/types"
)

type Sender struct {
	mux      sync.Mutex
	writer   *syslog.Writer
	enabled  bool
	protocol string
	server   string
	tag      string
	facility syslog.Priority
}

func NewSender(protocol, server, tag, facility string) *Sender {
	s := &Sender{
		protocol: protocol,
		server:   server,
		tag:      tag,
	}

	if facility == "" {
		facility = "daemon"
	}
	s.facility = getFacility(facility)

	if s.tag == "" {
		s.tag = "netbird"
	}

	return s
}

func getFacility(facility string) syslog.Priority {
	switch facility {
	case "kern":
		return syslog.LOG_KERN
	case "user":
		return syslog.LOG_USER
	case "mail":
		return syslog.LOG_MAIL
	case "daemon":
		return syslog.LOG_DAEMON
	case "auth":
		return syslog.LOG_AUTH
	case "syslog":
		return syslog.LOG_SYSLOG
	case "lpr":
		return syslog.LOG_LPR
	case "news":
		return syslog.LOG_NEWS
	case "uucp":
		return syslog.LOG_UUCP
	case "cron":
		return syslog.LOG_CRON
	case "authpriv":
		return syslog.LOG_AUTHPRIV
	case "ftp":
		return syslog.LOG_FTP
	case "local0":
		return syslog.LOG_LOCAL0
	case "local1":
		return syslog.LOG_LOCAL1
	case "local2":
		return syslog.LOG_LOCAL2
	case "local3":
		return syslog.LOG_LOCAL3
	case "local4":
		return syslog.LOG_LOCAL4
	case "local5":
		return syslog.LOG_LOCAL5
	case "local6":
		return syslog.LOG_LOCAL6
	case "local7":
		return syslog.LOG_LOCAL7
	default:
		return syslog.LOG_DAEMON
	}
}

func (s *Sender) Enable() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if s.enabled {
		return nil
	}

	if s.protocol == "" {
		s.protocol = "udp"
	}

	var err error
	s.writer, err = syslog.Dial(s.protocol, s.server, s.facility|syslog.LOG_INFO, s.tag)
	if err != nil {
		return err
	}

	s.enabled = true
	return nil
}

func (s *Sender) Disable() {
	s.mux.Lock()
	defer s.mux.Unlock()

	if s.writer != nil {
		s.writer.Close()
		s.writer = nil
	}
	s.enabled = false
}

func (s *Sender) Send(event *types.Event) error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if !s.enabled || s.writer == nil {
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("Flow Event: %s", string(data))
	return s.writer.Info(msg)
}

func (s *Sender) Close() {
	s.Disable()
}

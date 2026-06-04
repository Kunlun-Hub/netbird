package networktraffic

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/netbirdio/netbird/shared/management/http/api"
)

const (
	EndpointTypeUnknown      = "UNKNOWN"
	EndpointTypePeer         = "PEER"
	EndpointTypeHostResource = "HOST_RESOURCE"

	ConnectionTypeP2P    = "P2P"
	ConnectionTypeRouted = "ROUTED"

	SummaryBucketSeconds = 60
)

type Event struct {
	ID             string    `gorm:"primaryKey"`
	AccountID      string    `gorm:"index;index:idx_nt_account_timestamp,priority:1;index:idx_nt_account_flow_timestamp,priority:1"`
	FlowID         string    `gorm:"index;index:idx_nt_account_flow_timestamp,priority:2"`
	Timestamp      time.Time `gorm:"index;index:idx_nt_account_timestamp,priority:2;index:idx_nt_account_flow_timestamp,priority:3"`
	EventType      string    `gorm:"index"`
	Direction      string    `gorm:"index"`
	Protocol       int       `gorm:"index"`
	ConnectionType string    `gorm:"index"`
	ReporterID     string    `gorm:"index"`
	UserID         string    `gorm:"index"`

	SourceID          string `gorm:"index"`
	SourceType        string `gorm:"index"`
	SourceName        string
	SourceAddress     string `gorm:"index"`
	SourceDNSLabel    string
	SourceOS          string
	SourceCountryCode string
	SourceCityName    string

	DestinationID          string `gorm:"index"`
	DestinationType        string `gorm:"index"`
	DestinationName        string
	DestinationAddress     string `gorm:"index"`
	DestinationDNSLabel    string
	DestinationOS          string
	DestinationCountryCode string
	DestinationCityName    string

	PolicyID   string
	PolicyName string

	ICMPType int
	ICMPCode int

	RxBytes   int64
	RxPackets int64
	TxBytes   int64
	TxPackets int64

	DNSDomain    string
	DNSQueryType string
	DNSAnswers   []string `gorm:"serializer:json"`
	DNSRCode     string

	UserName  string
	UserEmail string
}

type FlowSummary struct {
	AccountID string `gorm:"primaryKey;index:idx_nt_flow_summaries_account_latest,priority:1;index:idx_nt_flow_summaries_account_client_latest,priority:1"`
	FlowID    string `gorm:"primaryKey"`
	ClientKey string `gorm:"index;index:idx_nt_flow_summaries_account_client_latest,priority:2"`

	FirstTimestamp  time.Time `gorm:"index"`
	LatestTimestamp time.Time `gorm:"index:idx_nt_flow_summaries_account_latest,priority:2;index:idx_nt_flow_summaries_account_client_latest,priority:4"`
	EventCount      int

	EventType      string `gorm:"index"`
	Direction      string `gorm:"index"`
	Protocol       int    `gorm:"index"`
	ConnectionType string `gorm:"index"`
	ReporterID     string `gorm:"index"`
	UserID         string `gorm:"index"`

	SourceID          string `gorm:"index;index:idx_nt_flow_summaries_account_client_latest,priority:3"`
	SourceType        string `gorm:"index"`
	SourceName        string `gorm:"index"`
	SourceAddress     string `gorm:"index"`
	SourceDNSLabel    string
	SourceOS          string
	SourceCountryCode string
	SourceCityName    string

	DestinationID          string `gorm:"index"`
	DestinationType        string `gorm:"index"`
	DestinationName        string
	DestinationAddress     string `gorm:"index"`
	DestinationDNSLabel    string
	DestinationOS          string
	DestinationCountryCode string
	DestinationCityName    string

	PolicyID   string
	PolicyName string

	ICMPType int
	ICMPCode int

	RxBytes   int64
	RxPackets int64
	TxBytes   int64
	TxPackets int64

	DNSDomain    string
	DNSQueryType string
	DNSAnswers   []string `gorm:"serializer:json"`
	DNSRCode     string

	UserName  string
	UserEmail string
}

type ClientGroup struct {
	ID              string
	Client          Endpoint
	UserID          string
	UserName        string
	UserEmail       string
	LatestTimestamp time.Time
	FlowCount       int64
	RxBytes         int64
	RxPackets       int64
	TxBytes         int64
	TxPackets       int64
	Protocols       []int
	Destinations    []Endpoint
}

type Endpoint struct {
	ID          string
	Type        string
	Name        string
	Address     string
	DNSLabel    string
	OS          string
	CountryCode string
	CityName    string
}

type SummaryPoint struct {
	Timestamp      time.Time
	BucketStart    time.Time
	BucketEnd      time.Time
	CoveredSeconds float64
	RxBytes        int64
	TxBytes        int64
	DownloadRate   float64
	UploadRate     float64
}

func NewFlowSummary(event *Event) *FlowSummary {
	return &FlowSummary{
		AccountID:              event.AccountID,
		FlowID:                 event.FlowID,
		ClientKey:              ClientKey(event),
		FirstTimestamp:         event.Timestamp,
		LatestTimestamp:        event.Timestamp,
		EventCount:             1,
		EventType:              event.EventType,
		Direction:              event.Direction,
		Protocol:               event.Protocol,
		ConnectionType:         event.ConnectionType,
		ReporterID:             event.ReporterID,
		UserID:                 event.UserID,
		SourceID:               event.SourceID,
		SourceType:             event.SourceType,
		SourceName:             event.SourceName,
		SourceAddress:          event.SourceAddress,
		SourceDNSLabel:         event.SourceDNSLabel,
		SourceOS:               event.SourceOS,
		SourceCountryCode:      event.SourceCountryCode,
		SourceCityName:         event.SourceCityName,
		DestinationID:          event.DestinationID,
		DestinationType:        event.DestinationType,
		DestinationName:        event.DestinationName,
		DestinationAddress:     event.DestinationAddress,
		DestinationDNSLabel:    event.DestinationDNSLabel,
		DestinationOS:          event.DestinationOS,
		DestinationCountryCode: event.DestinationCountryCode,
		DestinationCityName:    event.DestinationCityName,
		PolicyID:               event.PolicyID,
		PolicyName:             event.PolicyName,
		ICMPType:               event.ICMPType,
		ICMPCode:               event.ICMPCode,
		RxBytes:                event.RxBytes,
		RxPackets:              event.RxPackets,
		TxBytes:                event.TxBytes,
		TxPackets:              event.TxPackets,
		DNSDomain:              event.DNSDomain,
		DNSQueryType:           event.DNSQueryType,
		DNSAnswers:             event.DNSAnswers,
		DNSRCode:               event.DNSRCode,
		UserName:               event.UserName,
		UserEmail:              event.UserEmail,
	}
}

func ClientKey(event *Event) string {
	if event.SourceID != "" {
		return event.SourceID
	}
	if event.SourceName != "" {
		return event.SourceName
	}
	return addressHost(event.SourceAddress)
}

func addressHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	if strings.HasPrefix(address, "[") && strings.Contains(address, "]") {
		end := strings.Index(address, "]")
		if end > 1 {
			return address[1:end]
		}
	}
	lastColon := strings.LastIndex(address, ":")
	if lastColon > 0 {
		port := address[lastColon+1:]
		if port != "" && strings.Trim(port, "0123456789") == "" {
			return address[:lastColon]
		}
	}
	return address
}

func (s *FlowSummary) ToEvent() *Event {
	return &Event{
		AccountID:              s.AccountID,
		FlowID:                 s.FlowID,
		Timestamp:              s.LatestTimestamp,
		EventType:              s.EventType,
		Direction:              s.Direction,
		Protocol:               s.Protocol,
		ConnectionType:         s.ConnectionType,
		ReporterID:             s.ReporterID,
		UserID:                 s.UserID,
		SourceID:               s.SourceID,
		SourceType:             s.SourceType,
		SourceName:             s.SourceName,
		SourceAddress:          s.SourceAddress,
		SourceDNSLabel:         s.SourceDNSLabel,
		SourceOS:               s.SourceOS,
		SourceCountryCode:      s.SourceCountryCode,
		SourceCityName:         s.SourceCityName,
		DestinationID:          s.DestinationID,
		DestinationType:        s.DestinationType,
		DestinationName:        s.DestinationName,
		DestinationAddress:     s.DestinationAddress,
		DestinationDNSLabel:    s.DestinationDNSLabel,
		DestinationOS:          s.DestinationOS,
		DestinationCountryCode: s.DestinationCountryCode,
		DestinationCityName:    s.DestinationCityName,
		PolicyID:               s.PolicyID,
		PolicyName:             s.PolicyName,
		ICMPType:               s.ICMPType,
		ICMPCode:               s.ICMPCode,
		RxBytes:                s.RxBytes,
		RxPackets:              s.RxPackets,
		TxBytes:                s.TxBytes,
		TxPackets:              s.TxPackets,
		DNSDomain:              s.DNSDomain,
		DNSQueryType:           s.DNSQueryType,
		DNSAnswers:             s.DNSAnswers,
		DNSRCode:               s.DNSRCode,
		UserName:               s.UserName,
		UserEmail:              s.UserEmail,
	}
}

func FormatAddress(ip net.IP, port uint32) string {
	if ip == nil {
		return ""
	}
	if port == 0 {
		return ip.String()
	}
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))
}

func (e *Event) ToAPIResponse() *api.NetworkTrafficEvent {
	return &api.NetworkTrafficEvent{
		FlowId:      e.FlowID,
		Direction:   e.Direction,
		Protocol:    e.Protocol,
		ReporterId:  e.ReporterID,
		RxBytes:     int(e.RxBytes),
		RxPackets:   int(e.RxPackets),
		TxBytes:     int(e.TxBytes),
		TxPackets:   int(e.TxPackets),
		Dns:         e.toDNSInfo(),
		Source:      e.toEndpoint(true),
		Destination: e.toEndpoint(false),
		Policy:      api.NetworkTrafficPolicy{Id: e.PolicyID, Name: e.PolicyName},
		Icmp:        api.NetworkTrafficICMP{Type: e.ICMPType, Code: e.ICMPCode},
		User:        api.NetworkTrafficUser{Id: e.UserID, Name: e.UserName, Email: e.UserEmail},
		Events: []api.NetworkTrafficSubEvent{{
			Timestamp: e.Timestamp,
			Type:      e.EventType,
		}},
	}
}

func (e *Event) toDNSInfo() *api.NetworkTrafficDNSInfo {
	if e.DNSDomain == "" && e.DNSQueryType == "" && len(e.DNSAnswers) == 0 && e.DNSRCode == "" {
		return nil
	}

	answers := make([]string, 0, len(e.DNSAnswers))
	for _, answer := range e.DNSAnswers {
		answer = strings.TrimSpace(answer)
		if answer != "" {
			answers = append(answers, answer)
		}
	}

	return &api.NetworkTrafficDNSInfo{
		Domain:    stringPtr(e.DNSDomain),
		Query:     stringPtr(e.DNSDomain),
		QueryName: stringPtr(e.DNSDomain),
		Type:      stringPtr(e.DNSQueryType),
		QueryType: stringPtr(e.DNSQueryType),
		Answers:   &answers,
		Result:    &answers,
		Rcode:     stringPtr(e.DNSRCode),
	}
}

func (e *Event) toEndpoint(source bool) api.NetworkTrafficEndpoint {
	endpoint := api.NetworkTrafficEndpoint{
		Id:          e.DestinationID,
		Type:        e.DestinationType,
		Name:        e.DestinationName,
		Address:     e.DestinationAddress,
		DnsLabel:    stringPtr(e.DestinationDNSLabel),
		Os:          stringPtr(e.DestinationOS),
		GeoLocation: api.NetworkTrafficLocation{CountryCode: e.DestinationCountryCode, CityName: e.DestinationCityName},
	}

	if source {
		endpoint.Id = e.SourceID
		endpoint.Type = e.SourceType
		endpoint.Name = e.SourceName
		endpoint.Address = e.SourceAddress
		endpoint.DnsLabel = stringPtr(e.SourceDNSLabel)
		endpoint.Os = stringPtr(e.SourceOS)
		endpoint.GeoLocation = api.NetworkTrafficLocation{
			CountryCode: e.SourceCountryCode,
			CityName:    e.SourceCityName,
		}
	}

	return endpoint
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

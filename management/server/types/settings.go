package types

import (
	"net/netip"
	"slices"
	"strings"
	"time"
)

// LoginOption represents a single login option that can be enabled
type LoginOption string

const (
	// LoginMethodAll (Deprecated) - kept for backward compatibility
	LoginMethodAll = "all"
	// LoginMethodEmail (Deprecated) - kept for backward compatibility
	LoginMethodEmail = "email"
	// LoginMethodWeChatWork (Deprecated) - kept for backward compatibility
	LoginMethodWeChatWork = "wechatwork"
	// LoginOptionEmail represents email/password login
	LoginOptionEmail LoginOption = "email"
	// LoginOptionPrefix is the prefix for provider-specific login options
	LoginOptionPrefix LoginOption = "provider:"
)

// IsProviderLoginOption checks if a login option is a provider-specific option
func (l LoginOption) IsProviderLoginOption() bool {
	return strings.HasPrefix(string(l), string(LoginOptionPrefix))
}

// GetProviderIDFromLoginOption extracts the provider ID from a provider login option
func (l LoginOption) GetProviderIDFromLoginOption() string {
	if !l.IsProviderLoginOption() {
		return ""
	}
	return string(l[len(LoginOptionPrefix):])
}

// CreateProviderLoginOption creates a provider-specific login option
func CreateProviderLoginOption(providerID string) LoginOption {
	return LoginOption(string(LoginOptionPrefix) + providerID)
}

// Settings represents Account settings structure that can be modified via API and Dashboard
type Settings struct {
	// PeerLoginExpirationEnabled globally enables or disables peer login expiration
	PeerLoginExpirationEnabled bool

	// PeerLoginExpiration is a setting that indicates when peer login expires.
	// Applies to all peers that have Peer.LoginExpirationEnabled set to true.
	PeerLoginExpiration time.Duration

	// PeerInactivityExpirationEnabled globally enables or disables peer inactivity expiration
	PeerInactivityExpirationEnabled bool

	// PeerInactivityExpiration is a setting that indicates when peer inactivity expires.
	// Applies to all peers that have Peer.PeerInactivityExpirationEnabled set to true.
	PeerInactivityExpiration time.Duration

	// RegularUsersViewBlocked allows to block regular users from viewing even their own peers and some UI elements
	RegularUsersViewBlocked bool

	// GroupsPropagationEnabled allows to propagate auto groups from the user to the peer
	GroupsPropagationEnabled bool

	// JWTGroupsEnabled allows extract groups from JWT claim, which name defined in the JWTGroupsClaimName
	// and add it to account groups.
	JWTGroupsEnabled bool

	// JWTGroupsClaimName from which we extract groups name to add it to account groups
	JWTGroupsClaimName string

	// JWTAllowGroups list of groups to which users are allowed access
	JWTAllowGroups []string `gorm:"serializer:json"`

	// RoutingPeerDNSResolutionEnabled enabled the DNS resolution on the routing peers
	RoutingPeerDNSResolutionEnabled bool

	// DNSDomain is the custom domain for that account
	DNSDomain string

	// NetworkRange is the custom network range for that account
	NetworkRange netip.Prefix `gorm:"serializer:json"`
	// NetworkRangeV6 is the custom IPv6 network range for that account
	NetworkRangeV6 netip.Prefix `gorm:"serializer:json"`

	// PeerExposeEnabled enables or disables peer-initiated service expose
	PeerExposeEnabled bool
	// PeerExposeGroups list of peer group IDs allowed to expose services
	PeerExposeGroups []string `gorm:"serializer:json"`

	// Extra is a dictionary of Account settings
	Extra *ExtraSettings `gorm:"embedded;embeddedPrefix:extra_"`

	// LazyConnectionEnabled indicates if the experimental feature is enabled or disabled
	LazyConnectionEnabled bool `gorm:"default:false"`

	// AutoUpdateVersion client auto-update version
	AutoUpdateVersion string `gorm:"default:'disabled'"`

	// AutoUpdateAlways when true, updates are installed automatically in the background;
	// when false, updates require user interaction from the UI
	AutoUpdateAlways bool `gorm:"default:false"`

	// IPv6EnabledGroups is the list of group IDs whose peers receive IPv6 overlay addresses.
	// Peers not in any of these groups will not be allocated an IPv6 address.
	// Empty list means IPv6 is disabled for the account.
	// For new accounts this defaults to the All group.
	IPv6EnabledGroups []string `gorm:"serializer:json"`

	// EmbeddedIdpEnabled indicates if the embedded identity provider is enabled.
	// This is a runtime-only field, not stored in the database.
	EmbeddedIdpEnabled bool `gorm:"-"`

	// LocalAuthDisabled indicates if local (email/password) authentication is disabled.
	// This is a runtime-only field, not stored in the database.
	LocalAuthDisabled bool `gorm:"-"`

	// LoginMethod (deprecated) - kept for backward compatibility
	LoginMethod string `gorm:"default:'all'"`

	// EnabledLoginOptions is the list of enabled login options
	// Empty list means all options are enabled
	EnabledLoginOptions []LoginOption `gorm:"serializer:json;default:null"`
}

// Copy copies the Settings struct
func (s *Settings) Copy() *Settings {
	loginMethod := s.LoginMethod
	if loginMethod == "" {
		loginMethod = "all"
	}

	settings := &Settings{
		PeerLoginExpirationEnabled: s.PeerLoginExpirationEnabled,
		PeerLoginExpiration:        s.PeerLoginExpiration,
		JWTGroupsEnabled:           s.JWTGroupsEnabled,
		JWTGroupsClaimName:         s.JWTGroupsClaimName,
		GroupsPropagationEnabled:   s.GroupsPropagationEnabled,
		JWTAllowGroups:             s.JWTAllowGroups,
		RegularUsersViewBlocked:    s.RegularUsersViewBlocked,

		PeerInactivityExpirationEnabled: s.PeerInactivityExpirationEnabled,
		PeerInactivityExpiration:        s.PeerInactivityExpiration,

		RoutingPeerDNSResolutionEnabled: s.RoutingPeerDNSResolutionEnabled,
		PeerExposeEnabled:               s.PeerExposeEnabled,
		PeerExposeGroups:                slices.Clone(s.PeerExposeGroups),
		LazyConnectionEnabled:           s.LazyConnectionEnabled,
		DNSDomain:                       s.DNSDomain,
		NetworkRange:                    s.NetworkRange,
		NetworkRangeV6:                  s.NetworkRangeV6,
		AutoUpdateVersion:               s.AutoUpdateVersion,
		AutoUpdateAlways:                s.AutoUpdateAlways,
		IPv6EnabledGroups:               slices.Clone(s.IPv6EnabledGroups),
		EmbeddedIdpEnabled:              s.EmbeddedIdpEnabled,
		LocalAuthDisabled:               s.LocalAuthDisabled,
		LoginMethod:                     loginMethod,
		EnabledLoginOptions:             slices.Clone(s.EnabledLoginOptions),
	}
	if s.Extra != nil {
		settings.Extra = s.Extra.Copy()
	}
	return settings
}

type ExtraSettings struct {
	// PeerApprovalEnabled enables or disables the need for peers bo be approved by an administrator
	PeerApprovalEnabled bool

	// UserApprovalRequired enables or disables the need for users joining via domain matching to be approved by an administrator
	UserApprovalRequired bool

	// IntegratedValidator is the string enum for the integrated validator type
	IntegratedValidator string
	// IntegratedValidatorGroups list of group IDs to be used with integrated approval configurations
	IntegratedValidatorGroups []string `gorm:"serializer:json"`

	FlowEnabled              bool
	FlowGroups               []string `gorm:"serializer:json"`
	FlowPacketCounterEnabled bool
	FlowENCollectionEnabled  bool
	FlowDnsCollectionEnabled bool

	// FlowLocalStorageEnabled enables or disables local storage of flow logs
	FlowLocalStorageEnabled bool
	// FlowLocalStoragePath sets the path where flow logs are stored locally
	FlowLocalStoragePath string
	// FlowLocalStorageMaxSizeMB sets the max size of local log files in MB
	FlowLocalStorageMaxSizeMB int
	// FlowLocalStorageMaxFiles sets the max number of local log files to keep
	FlowLocalStorageMaxFiles int

	// FlowSyslogEnabled enables or disables sending flow logs to a syslog server
	FlowSyslogEnabled bool
	// FlowSyslogServer sets the syslog server address (host:port)
	FlowSyslogServer string
	// FlowSyslogProtocol sets the syslog protocol (udp/tcp)
	FlowSyslogProtocol string
	// FlowSyslogFacility sets the syslog facility
	FlowSyslogFacility string
	// FlowSyslogTag sets the syslog tag
	FlowSyslogTag string
}

// Copy copies the ExtraSettings struct
func (e *ExtraSettings) Copy() *ExtraSettings {
	return &ExtraSettings{
		PeerApprovalEnabled:       e.PeerApprovalEnabled,
		UserApprovalRequired:      e.UserApprovalRequired,
		IntegratedValidatorGroups: slices.Clone(e.IntegratedValidatorGroups),
		IntegratedValidator:       e.IntegratedValidator,
		FlowEnabled:               e.FlowEnabled,
		FlowGroups:                slices.Clone(e.FlowGroups),
		FlowPacketCounterEnabled:  e.FlowPacketCounterEnabled,
		FlowENCollectionEnabled:   e.FlowENCollectionEnabled,
		FlowDnsCollectionEnabled:  e.FlowDnsCollectionEnabled,
		FlowLocalStorageEnabled:   e.FlowLocalStorageEnabled,
		FlowLocalStoragePath:      e.FlowLocalStoragePath,
		FlowLocalStorageMaxSizeMB: e.FlowLocalStorageMaxSizeMB,
		FlowLocalStorageMaxFiles:  e.FlowLocalStorageMaxFiles,
		FlowSyslogEnabled:         e.FlowSyslogEnabled,
		FlowSyslogServer:          e.FlowSyslogServer,
		FlowSyslogProtocol:        e.FlowSyslogProtocol,
		FlowSyslogFacility:        e.FlowSyslogFacility,
		FlowSyslogTag:             e.FlowSyslogTag,
	}
}

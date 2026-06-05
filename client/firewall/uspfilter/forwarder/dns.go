package forwarder

import nbdns "github.com/netbirdio/netbird/dns"

func isDNSPort(port uint16) bool {
	return port == nbdns.DefaultDNSPort ||
		port == nbdns.ForwarderClientPort ||
		port == nbdns.ForwarderServerPort
}

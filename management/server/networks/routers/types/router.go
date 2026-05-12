package types

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/netip"

	"github.com/rs/xid"

	"github.com/netbirdio/netbird/management/server/networks/types"
	"github.com/netbirdio/netbird/shared/management/http/api"
)

type NetworkRouter struct {
	ID         string `gorm:"primaryKey"`
	NetworkID  string `gorm:"index"`
	AccountID  string `gorm:"index"`
	Peer       string
	PeerGroups []string `gorm:"serializer:json"`
	// AdvertisedRoutes optionally overrides the resource prefix advertised through this router.
	// Empty means the router advertises the network resource prefix as before.
	AdvertisedRoutes []netip.Prefix `gorm:"serializer:json"`
	ExcludedRoutes   []netip.Prefix `gorm:"serializer:json"`
	Masquerade       bool
	Metric           int
	Enabled          bool
}

func NewNetworkRouter(accountID string, networkID string, peer string, peerGroups []string, masquerade bool, metric int, enabled bool) (*NetworkRouter, error) {
	r := &NetworkRouter{
		ID:               xid.New().String(),
		AccountID:        accountID,
		NetworkID:        networkID,
		Peer:             peer,
		PeerGroups:       peerGroups,
		AdvertisedRoutes: nil,
		ExcludedRoutes:   nil,
		Masquerade:       masquerade,
		Metric:           metric,
		Enabled:          enabled,
	}

	if err := r.Validate(); err != nil {
		return nil, err
	}

	return r, nil
}

func (n *NetworkRouter) Validate() error {
	if n.Peer != "" && len(n.PeerGroups) > 0 {
		return errors.New("peer and peer_groups cannot be set at the same time")
	}

	if n.Peer == "" && len(n.PeerGroups) == 0 {
		return errors.New("either peer or peer_groups must be provided")
	}

	for _, advertisedRoute := range n.AdvertisedRoutes {
		if !advertisedRoute.IsValid() {
			return errors.New("advertised_routes contains an invalid CIDR")
		}
	}

	for _, excludedRoute := range n.ExcludedRoutes {
		if !excludedRoute.IsValid() {
			return errors.New("excluded_routes contains an invalid CIDR")
		}
	}

	return nil
}

func (n *NetworkRouter) ToAPIResponse() *api.NetworkRouter {
	advertisedRoutes := prefixesToStrings(n.AdvertisedRoutes)
	excludedRoutes := prefixesToStrings(n.ExcludedRoutes)

	return &api.NetworkRouter{
		Id:               n.ID,
		Peer:             &n.Peer,
		PeerGroups:       &n.PeerGroups,
		AdvertisedRoutes: &advertisedRoutes,
		ExcludedRoutes:   &excludedRoutes,
		Masquerade:       n.Masquerade,
		Metric:           n.Metric,
		Enabled:          n.Enabled,
	}
}

func (n *NetworkRouter) FromAPIRequest(req *api.NetworkRouterRequest) {
	if req.Peer != nil {
		n.Peer = *req.Peer
	}

	if req.PeerGroups != nil {
		n.PeerGroups = *req.PeerGroups
	}

	if req.AdvertisedRoutes != nil {
		n.AdvertisedRoutes = stringsToPrefixes(*req.AdvertisedRoutes)
	}

	if req.ExcludedRoutes != nil {
		n.ExcludedRoutes = stringsToPrefixes(*req.ExcludedRoutes)
	}

	n.Masquerade = req.Masquerade
	n.Metric = req.Metric
	n.Enabled = req.Enabled
}

func (n *NetworkRouter) Copy() *NetworkRouter {
	return &NetworkRouter{
		ID:               n.ID,
		NetworkID:        n.NetworkID,
		AccountID:        n.AccountID,
		Peer:             n.Peer,
		PeerGroups:       n.PeerGroups,
		AdvertisedRoutes: n.AdvertisedRoutes,
		ExcludedRoutes:   n.ExcludedRoutes,
		Masquerade:       n.Masquerade,
		Metric:           n.Metric,
		Enabled:          n.Enabled,
	}
}

func (n *NetworkRouter) EventMeta(network *types.Network) map[string]any {
	return map[string]any{
		"network_name":      network.Name,
		"network_id":        network.ID,
		"peer":              n.Peer,
		"peer_groups":       n.PeerGroups,
		"advertised_routes": prefixesToStrings(n.AdvertisedRoutes),
		"excluded_routes":   prefixesToStrings(n.ExcludedRoutes),
	}
}

func prefixesToStrings(prefixes []netip.Prefix) []string {
	result := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		if prefix.IsValid() {
			result = append(result, prefix.Masked().String())
		}
	}
	return result
}

func stringsToPrefixes(values []string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			if addr, addrErr := netip.ParseAddr(value); addrErr == nil {
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			} else {
				result = append(result, netip.Prefix{})
				continue
			}
		}
		result = append(result, prefix.Masked())
	}
	return result
}

func (n *NetworkRouter) RoutePrefixes(defaultPrefix netip.Prefix) []netip.Prefix {
	if !defaultPrefix.IsValid() {
		return nil
	}

	advertisedRoutes := n.AdvertisedRoutes
	if len(advertisedRoutes) == 0 {
		advertisedRoutes = []netip.Prefix{defaultPrefix}
	}

	var result []netip.Prefix
	for _, advertisedRoute := range advertisedRoutes {
		if !advertisedRoute.IsValid() || advertisedRoute.Addr().Is6() != defaultPrefix.Addr().Is6() {
			continue
		}
		for _, prefix := range subtractPrefixes(advertisedRoute.Masked(), n.ExcludedRoutes) {
			result = append(result, prefix)
		}
	}

	return result
}

func subtractPrefixes(base netip.Prefix, exclusions []netip.Prefix) []netip.Prefix {
	remaining := []netip.Prefix{base.Masked()}
	for _, exclusion := range exclusions {
		if !exclusion.IsValid() || exclusion.Addr().Is6() != base.Addr().Is6() {
			continue
		}

		next := make([]netip.Prefix, 0, len(remaining))
		for _, prefix := range remaining {
			next = append(next, subtractPrefix(prefix, exclusion.Masked())...)
		}
		remaining = next
	}
	return remaining
}

func subtractPrefix(base, exclusion netip.Prefix) []netip.Prefix {
	if !base.Overlaps(exclusion) {
		return []netip.Prefix{base}
	}
	if exclusion.Bits() <= base.Bits() && exclusion.Contains(base.Addr()) {
		return nil
	}
	if base.Bits() == base.Addr().BitLen() {
		return nil
	}

	left, right, err := splitPrefix(base)
	if err != nil {
		return []netip.Prefix{base}
	}

	result := subtractPrefix(left, exclusion)
	result = append(result, subtractPrefix(right, exclusion)...)
	return result
}

func splitPrefix(prefix netip.Prefix) (netip.Prefix, netip.Prefix, error) {
	bits := prefix.Bits()
	nextBits := bits + 1
	if nextBits > prefix.Addr().BitLen() {
		return netip.Prefix{}, netip.Prefix{}, fmt.Errorf("cannot split host prefix %s", prefix)
	}

	left := netip.PrefixFrom(prefix.Addr(), nextBits).Masked()
	rightAddr, err := addPrefixBlock(left.Addr(), nextBits)
	if err != nil {
		return netip.Prefix{}, netip.Prefix{}, err
	}

	return left, netip.PrefixFrom(rightAddr, nextBits).Masked(), nil
}

func addPrefixBlock(addr netip.Addr, bits int) (netip.Addr, error) {
	if addr.Is4() {
		raw := addr.As4()
		value := binary.BigEndian.Uint32(raw[:])
		value = uint32(uint64(value) + (uint64(1) << (32 - bits)))
		var next [4]byte
		binary.BigEndian.PutUint32(next[:], value)
		return netip.AddrFrom4(next), nil
	}

	raw := addr.As16()
	value := new(big.Int).SetBytes(raw[:])
	value.Add(value, new(big.Int).Lsh(big.NewInt(1), uint(128-bits)))
	bytes := value.FillBytes(make([]byte, 16))
	var next [16]byte
	copy(next[:], bytes)
	return netip.AddrFrom16(next), nil
}

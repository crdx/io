package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

type network struct {
	prefix netip.Prefix
	name   string
}

var networks = []network{
	{netip.MustParsePrefix("0.0.0.0/8"), "this network"},
	{netip.MustParsePrefix("100.64.0.0/10"), "carrier-grade NAT space"},
	{netip.MustParsePrefix("192.0.0.0/24"), "IETF protocol assignment space"},
	{netip.MustParsePrefix("192.0.2.0/24"), "documentation space"},
	{netip.MustParsePrefix("198.18.0.0/15"), "benchmarking space"},
	{netip.MustParsePrefix("198.51.100.0/24"), "documentation space"},
	{netip.MustParsePrefix("203.0.113.0/24"), "documentation space"},
	{netip.MustParsePrefix("240.0.0.0/4"), "reserved space"},
	{netip.MustParsePrefix("2001::/32"), "a Teredo tunnel to an embedded IPv4 address"},
	{netip.MustParsePrefix("2002::/16"), "a 6to4 tunnel to an embedded IPv4 address"},
	{netip.MustParsePrefix("64:ff9b::/96"), "a NAT64 translation of an embedded IPv4 address"},
	{netip.MustParsePrefix("2001:db8::/32"), "documentation space"},
	{netip.MustParsePrefix("fec0::/10"), "deprecated site-local space"},
}

func networkNameFor(address netip.Addr) string {
	address = address.Unmap()

	switch {
	case !address.IsValid():
		return "not an address"
	case address.IsUnspecified():
		return "the unspecified address"
	case address.IsLoopback():
		return "a loopback address"
	case address.IsPrivate():
		return "a private address"
	case address.IsLinkLocalUnicast(), address.IsLinkLocalMulticast():
		return "a link-local address"
	case address.IsMulticast(), address.IsInterfaceLocalMulticast():
		return "a multicast address"
	}

	for _, network := range networks {
		if network.prefix.Contains(address) {
			return network.name
		}
	}

	return ""
}

func refuseReservedAddress(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("could not read the address %s: %w", address, err)
	}

	parsedAddress, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("could not read the address %s: %w", host, err)
	}

	if networkName := networkNameFor(parsedAddress); networkName != "" {
		return fmt.Errorf(
			"%s is %s, and the web tools reach only the public internet",
			parsedAddress, networkName,
		)
	}

	return nil
}

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 10 * time.Second
	keepAlivePeriod  = 30 * time.Second
	idleTimeout      = 90 * time.Second
	continueTimeout  = time.Second
	idleConnections  = 10
)

func publicTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: keepAlivePeriod,
		Control:   refuseReservedAddress,
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          idleConnections,
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   handshakeTimeout,
		ExpectContinueTimeout: continueTimeout,
	}
}

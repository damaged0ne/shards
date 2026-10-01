package probes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// DefaultBlockedNetworks are never probed unless explicitly allowed: probes run from the server,
// so without this anyone allowed to create a probe could read cloud instance metadata (SSRF).
var DefaultBlockedNetworks = []string{
	"169.254.0.0/16",    // IPv4 link-local, incl. the AWS/GCP/Azure/OCI metadata endpoint 169.254.169.254
	"fe80::/10",         // IPv6 link-local
	"fd00:ec2::254/128", // AWS IMDS over IPv6
}

var ErrBlockedAddress = errors.New("the address is blocked")

// Guard restricts the addresses probes may connect to.
type Guard struct {
	blocked []netip.Prefix
	allowed []netip.Prefix
}

// NewGuard creates a guard blocking DefaultBlockedNetworks except the allowed networks (CIDRs or IPs).
func NewGuard(allowed []string) (*Guard, error) {
	g := &Guard{}
	for _, s := range DefaultBlockedNetworks {
		g.blocked = append(g.blocked, netip.MustParsePrefix(s))
	}
	for _, s := range allowed {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := parsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("invalid network %q: %w", s, err)
		}
		g.allowed = append(g.allowed, p)
	}
	return g, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// Check returns ErrBlockedAddress if the address must not be probed.
func (g *Guard) Check(addr netip.Addr) error {
	addr = addr.Unmap().WithZone("")
	for _, p := range g.allowed {
		if p.Contains(addr) {
			return nil
		}
	}
	for _, p := range g.blocked {
		if p.Contains(addr) {
			return fmt.Errorf("%w: %s is in %s (use --probes-allowed-networks to allow it)", ErrBlockedAddress, addr, p)
		}
	}
	return nil
}

type dialTimings struct {
	dns     float64
	connect float64
}

// dial resolves the host, checks every address and connects to the first allowed one that accepts the
// connection. The checked IP is dialed directly, so a DNS response can't change between the check and
// the connection (DNS rebinding).
func (g *Guard) dial(ctx context.Context, network, address string, t *dialTimings) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	start := now()
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		ipNetwork := "ip"
		switch network {
		case "tcp4", "udp4":
			ipNetwork = "ip4"
		case "tcp6", "udp6":
			ipNetwork = "ip6"
		}
		addrs, err = net.DefaultResolver.LookupNetIP(ctx, ipNetwork, host)
		if err != nil {
			return nil, err
		}
	}
	if t != nil {
		t.dns += since(start)
	}
	var allowed []netip.Addr
	var checkErr error
	for _, a := range addrs {
		if err := g.Check(a); err != nil {
			checkErr = err
			continue
		}
		allowed = append(allowed, a)
	}
	if len(allowed) == 0 {
		if checkErr != nil {
			return nil, checkErr
		}
		return nil, fmt.Errorf("no addresses found for %s", host)
	}
	start = now()
	d := net.Dialer{}
	var lastErr error
	for _, a := range allowed {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			if t != nil {
				t.connect += since(start)
			}
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

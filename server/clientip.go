package main

import (
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

// ---------------------------------------------------------------------
// Who is really calling: the client's address behind a proxy
// ---------------------------------------------------------------------

// Loopback is trusted by default because the bundled load balancer
// (server/lb) runs on the same machine as the API.
const defaultTrustedProxies = "127.0.0.0/8,::1/128"

// trustedProxies are the addresses allowed to tell us, through the
// X-Forwarded-For header, which client they are forwarding a request for.
// main() replaces the default with TRUSTED_PROXIES from server/.env.
var trustedProxies = mustParseProxies(defaultTrustedProxies)

// parseProxies reads a comma-separated list of addresses and CIDR ranges,
// for example "127.0.0.1, 172.16.0.0/12". The single word "none" trusts
// nobody.
func parseProxies(list string) ([]netip.Prefix, error) {
	if strings.EqualFold(strings.TrimSpace(list), "none") {
		return nil, nil
	}
	var out []netip.Prefix
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			prefix, err := netip.ParsePrefix(item)
			if err != nil {
				return nil, err
			}
			out = append(out, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(item)
		if err != nil {
			return nil, err
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func mustParseProxies(list string) []netip.Prefix {
	prefixes, err := parseProxies(list)
	if err != nil {
		panic(err)
	}
	return prefixes
}

// loadTrustedProxies applies TRUSTED_PROXIES. A typo here would either break
// rate limiting or let clients forge their address, so it stops the server
// instead of being ignored.
func loadTrustedProxies() {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if raw == "" {
		return // keep the default
	}
	prefixes, err := parseProxies(raw)
	if err != nil {
		log.Fatalf("TRUSTED_PROXIES is not a list of addresses or CIDR ranges: %v", err)
	}
	trustedProxies = prefixes
}

func isTrustedProxy(addr netip.Addr) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// parseIP accepts "1.2.3.4", "1.2.3.4:5678", "::1" and "[::1]:5678".
func parseIP(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	addr, err := netip.ParseAddr(s)
	if err != nil {
		addrPort, err := netip.ParseAddrPort(s)
		if err != nil {
			return netip.Addr{}, false
		}
		addr = addrPort.Addr()
	}
	// ::ffff:1.2.3.4 and 1.2.3.4 are the same client.
	return addr.Unmap().WithZone(""), true
}

// clientIP returns the address of the client that sent this request.
//
// r.RemoteAddr is whoever opened the TCP connection. Behind the load
// balancer that is always the load balancer, so every user would share one
// rate limit and the audit log would show the same address for everyone.
// The balancer passes the real address along in X-Forwarded-For, but that
// header is only text and any client can send it too. So:
//
//   - it is ignored unless the connection itself comes from a trusted proxy;
//   - it is read from the right, skipping our own proxies. Each proxy appends
//     the address it saw, so the first address from the right that is not
//     one of ours was written by our outermost proxy and is the client.
//     Everything to its left came from the client and is never used.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, ok := parseIP(host)
	if !ok {
		return host
	}
	if !isTrustedProxy(peer) {
		return peer.String()
	}

	client := peer
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, ok := parseIP(hops[i])
		if !ok {
			break // not an address: nothing further left can be trusted
		}
		client = addr
		if !isTrustedProxy(addr) {
			break
		}
	}
	return client.String()
}

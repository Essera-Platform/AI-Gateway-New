// Package clientip takes a request's client address from the header a trusted edge sets, such
// as Cloudflare's CF-Connecting-IP, so sessions, audit, sign-in throttling and rate limits see
// the person rather than the load balancer or the edge in front of it.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Wrap returns next unchanged unless ESSERA_CLIENT_IP_HEADER names a header. Then a request whose
// peer is a trusted proxy (TRUSTED_PROXIES) and that carries one valid address in that header
// reaches next from that address: RemoteAddr becomes it and X-Forwarded-For and X-Real-IP are
// dropped, so gin's ClientIP returns it everywhere. The header of any other peer is ignored,
// since anyone can send it.
func Wrap(next http.Handler) (http.Handler, error) {
	header := strings.TrimSpace(os.Getenv("ESSERA_CLIENT_IP_HEADER"))
	if header == "" {
		return next, nil
	}
	proxies, _, err := common.ResolveTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return nil, err
	}
	trusted := make([]netip.Prefix, 0, len(proxies))
	for _, proxy := range proxies {
		prefix, err := netip.ParsePrefix(proxy)
		if err != nil {
			addr, addrErr := netip.ParseAddr(proxy)
			if addrErr != nil {
				return nil, fmt.Errorf("invalid TRUSTED_PROXIES entry %q", proxy)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		trusted = append(trusted, prefix)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if client, ok := fromTrustedProxy(r, header, trusted); ok {
			_, port, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				port = "0"
			}
			r.RemoteAddr = net.JoinHostPort(client.String(), port)
			r.Header.Del("X-Forwarded-For")
			r.Header.Del("X-Real-IP")
		}
		next.ServeHTTP(w, r)
	}), nil
}

func fromTrustedProxy(r *http.Request, header string, trusted []netip.Prefix) (netip.Addr, bool) {
	values := r.Header.Values(header)
	if len(values) != 1 {
		return netip.Addr{}, false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	peer = peer.Unmap()
	if !slices.ContainsFunc(trusted, func(prefix netip.Prefix) bool { return prefix.Contains(peer) }) {
		return netip.Addr{}, false
	}
	client, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil {
		return netip.Addr{}, false
	}
	return client.Unmap(), true
}

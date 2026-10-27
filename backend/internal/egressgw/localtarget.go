package egressgw

import (
	"errors"
	"fmt"
	"net/netip"
	"syscall"
)

// ErrLocalTarget is returned when an egress destination is a loopback,
// unspecified or link-local address. The CONNECT host comes from a sandbox, so
// without this check a sandbox could reach the gateway's loopback services or
// the IPv4 link-local metadata endpoint with the gateway's network identity.
var ErrLocalTarget = errors.New("egress target is a local or link-local address")

// RefuseLocalTarget is a net.Dialer Control function that refuses loopback,
// unspecified and link-local addresses. It runs after DNS resolution, so it also
// catches localhost and an empty host, which Go dials as the local system.
func RefuseLocalTarget(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: unparseable address %q", ErrLocalTarget, address)
	}
	if isLocalAddr(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrLocalTarget, address)
	}
	return nil
}

func isLocalAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast()
}

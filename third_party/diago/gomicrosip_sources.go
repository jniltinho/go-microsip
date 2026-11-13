package diago

import (
	"context"
	"net"
	"strings"

	"github.com/emiago/sipgo"
)

// go-microsip: a listener that drops what does not come from an accepted source before the SIP
// stack sees it. The stack matches a CANCEL to its INVITE by the Via it carries, whatever address
// it came from, and acts on it before any request handler runs: a filter in a handler is too late
// for that request.

// serveAccepted listens on a UDP or TCP address and serves it through the filter. It reports
// false for a network it does not handle, which is then served as upstream does.
func serveAccepted(ctx context.Context, server *sipgo.Server, network, addr string, accept func(net.Addr) bool) (bool, error) {
	ready := func(local string) {
		if f, ok := ctx.Value(sipgo.ListenReadyCtxKey).(sipgo.ListenReadyFuncCtxValue); ok {
			f(network, local)
		}
	}
	switch strings.ToLower(network) {
	case "udp", "udp4", "udp6":
		laddr, err := net.ResolveUDPAddr(network, addr)
		if err != nil {
			return true, err
		}
		conn, err := net.ListenUDP(network, laddr)
		if err != nil {
			return true, err
		}
		go func() { <-ctx.Done(); _ = conn.Close() }()
		ready(conn.LocalAddr().String())
		return true, server.ServeUDP(&acceptedPackets{PacketConn: conn, accept: accept})
	case "tcp", "tcp4", "tcp6":
		laddr, err := net.ResolveTCPAddr(network, addr)
		if err != nil {
			return true, err
		}
		l, err := net.ListenTCP(network, laddr)
		if err != nil {
			return true, err
		}
		go func() { <-ctx.Done(); _ = l.Close() }()
		ready(l.Addr().String())
		return true, server.ServeTCP(&acceptedConnections{Listener: l, accept: accept})
	}
	return false, nil
}

// acceptedPackets gives its reader only the packets of accepted sources.
type acceptedPackets struct {
	net.PacketConn
	accept func(net.Addr) bool
}

func (c *acceptedPackets) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, from, err := c.PacketConn.ReadFrom(b)
		if err != nil || c.accept(from) {
			return n, from, err
		}
	}
}

// acceptedConnections gives its server only the connections of accepted sources.
type acceptedConnections struct {
	net.Listener
	accept func(net.Addr) bool
}

func (l *acceptedConnections) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || l.accept(conn.RemoteAddr()) {
			return conn, err
		}
		_ = conn.Close()
	}
}

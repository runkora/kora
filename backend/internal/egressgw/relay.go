package egressgw

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// relayDialTimeout bounds the upstream TCP connect on the raw-relay path.
const relayDialTimeout = 10 * time.Second

// relayIdleTimeout tears down a relayed tunnel after this much inactivity in
// both directions. It reclaims abandoned tunnels without cutting off
// legitimately long-lived but quiet sessions (interactive SSH, a pooled DB
// connection) too aggressively.
const relayIdleTimeout = 10 * time.Minute

// serveRelay handles a CONNECT to a non-intercepted port. It opens a plain TCP
// tunnel to the upstream and shuttles bytes both ways — no TLS termination, no
// HTTP parsing, no credential injection — emitting open/close audit logs.
func (h *ConnectHandler) serveRelay(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, host, port string) {
	target := net.JoinHostPort(host, port)

	// Dial the upstream BEFORE hijack so a failure surfaces as a clean HTTP
	// error to the tool rather than a silently-closed tunnel.
	dialer := &net.Dialer{Timeout: relayDialTimeout}
	upstream, dialErr := dialer.DialContext(ctx, "tcp", target)
	if dialErr != nil {
		h.logger.Warn(ctx, "egress relay upstream dial failed",
			logging.Field{Key: "host", Value: host},
			logging.Field{Key: "port", Value: port},
			logging.Field{Key: "error", Value: dialErr.Error()},
		)
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "upstream unreachable", nil),
			dialErr)
		return
	}
	defer func() { _ = upstream.Close() }()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		platformapi.ErrorResponse(ctx, w, h.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError,
				"server does not support connection hijacking", nil), nil)
		return
	}
	clientConn, bufrw, hijackErr := hijacker.Hijack()
	if hijackErr != nil {
		h.logger.Error(ctx, "hijack failed (relay)", logging.Field{Key: "error", Value: hijackErr.Error()})
		return
	}
	defer func() { _ = clientConn.Close() }()

	if _, err := bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := bufrw.Flush(); err != nil {
		return
	}

	h.logger.Info(ctx, "egress relay opened",
		logging.Field{Key: "host", Value: host},
		logging.Field{Key: "port", Value: port},
		logging.Field{Key: "principal", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
	)

	// Forward any bytes the tool pipelined after CONNECT (rare — clients
	// normally wait for the 200 — but the hijacked reader may already hold them).
	if n := bufrw.Reader.Buffered(); n > 0 {
		if pending, peekErr := bufrw.Peek(n); peekErr == nil {
			if _, wErr := upstream.Write(pending); wErr != nil {
				return
			}
			_, _ = bufrw.Discard(n)
		}
	}

	start := time.Now()
	sent, recv := relayTCP(clientConn, upstream, relayIdleTimeout)

	h.logger.Info(ctx, "egress relay closed",
		logging.Field{Key: "host", Value: host},
		logging.Field{Key: "port", Value: port},
		logging.Field{Key: "principal", Value: claims.Subject},
		logging.Field{Key: "tenant_id", Value: claims.Orbit.TenantID},
		logging.Field{Key: "bytes_sent", Value: sent},
		logging.Field{Key: "bytes_recv", Value: recv},
		logging.Field{Key: "duration_ms", Value: time.Since(start).Milliseconds()},
	)
}

// relayTCP shuttles bytes between the agent (client) and the upstream until
// either side closes or the idle timeout fires. It returns bytes sent
// (client→upstream) and received (upstream→client). Each direction half-closes
// its write side on EOF so protocols that signal end-of-stream that way (e.g. a
// graceful SQL/SSH shutdown) propagate correctly.
func relayTCP(client, upstream net.Conn, idle time.Duration) (sent, recv int64) {
	c := &idleConn{Conn: client, idle: idle}
	u := &idleConn{Conn: upstream, idle: idle}

	done := make(chan struct{}, 2)
	go func() {
		sent, _ = io.Copy(u, c) // client → upstream
		closeWrite(upstream)
		done <- struct{}{}
	}()
	go func() {
		recv, _ = io.Copy(c, u) // upstream → client
		closeWrite(client)
		done <- struct{}{}
	}()
	<-done
	<-done
	return sent, recv
}

// closeWrite half-closes the write side of a TCP connection if supported, so
// the peer sees EOF while the other direction keeps flowing.
func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

// idleConn refreshes a read/write deadline on every I/O so an idle relay is
// torn down after `idle` of no activity in either direction, while an active
// one is never timed out.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(c.idle))
	return c.Conn.Write(b)
}

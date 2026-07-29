package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"opentela/internal/protocol"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	libp2ppeer "github.com/libp2p/go-libp2p/core/peer"

	gostream "github.com/libp2p/go-libp2p-gostream"
	p2phttp "github.com/libp2p/go-libp2p-http"
)

// libp2pHTTPRoundTripper turns "libp2p://<peer-id>/path" requests into
// HTTP/1.1 requests over a libp2p stream, with stdlib keep-alive pooling.
//
// The third-party github.com/libp2p/go-libp2p-http opens a fresh stream
// per request and closes it on response-body Close, which over a relay
// circuit means a new circuit handshake every probe sample. This wrapper
// uses the same gostream + /libp2p-http protocol but threads it through
// stdlib *http.Transport so HTTP keep-alive can pin one stream per peer
// across requests.
//
// The URL scheme is rewritten libp2p:// -> http:// before delegating;
// stdlib http.Transport rejects unknown schemes.
type libp2pHTTPRoundTripper struct {
	inner *http.Transport
}

type libp2pDialMode struct {
	allowLimitedFallback bool
	forceDirect          bool
	requireTrustedDirect bool
}

func newLibp2pHTTPRoundTripper(h host.Host) *libp2pHTTPRoundTripper {
	return newLibp2pHTTPRoundTripperWithMode(h, libp2pDialMode{allowLimitedFallback: true})
}

func newTrustedLibp2pHTTPRoundTripper(h host.Host) *libp2pHTTPRoundTripper {
	return newLibp2pHTTPRoundTripperWithMode(h, libp2pDialMode{
		forceDirect:          true,
		requireTrustedDirect: true,
	})
}

func newLibp2pHTTPRoundTripperWithMode(h host.Host, mode libp2pDialMode) *libp2pHTTPRoundTripper {
	rt := &libp2pHTTPRoundTripper{}
	rt.inner = &http.Transport{
		DialContext: func(ctx context.Context, _ string, addr string) (net.Conn, error) {
			hostPart, _, err := net.SplitHostPort(addr)
			if err != nil {
				hostPart = addr
			}
			pid, err := libp2ppeer.Decode(hostPart)
			if err != nil {
				return nil, fmt.Errorf("libp2pHTTP: invalid peer id %q: %w", hostPart, err)
			}
			// Only allow circuit-relay (Limited) streams when no direct
			// connection exists. With Connected, the plain context lets
			// libp2p use the direct path; otherwise we explicitly opt in
			// to circuit (matching ping at p2p/protocol/ping/ping.go:117).
			// Without this guard, libp2p picks the Limited conn even when
			// a direct one is available, capping throughput at the relay's
			// per-flow rate.
			dialCtx := ctx
			switch {
			case mode.forceDirect:
				dialCtx = network.WithForceDirectDial(ctx, "trusted-libp2p-http")
			case mode.allowLimitedFallback && h.Network().Connectedness(pid) != network.Connected:
				dialCtx = network.WithAllowLimitedConn(ctx, "libp2p-http")
			}
			conn, err := gostream.Dial(dialCtx, h, pid, p2phttp.DefaultP2PProtocol)
			if err != nil {
				return nil, err
			}
			if mode.requireTrustedDirect && !protocol.HasTrustedDirectConnection(pid.String()) {
				_ = conn.Close()
				return nil, fmt.Errorf("trusted direct connection unavailable for %s", pid.String())
			}
			return conn, nil
		},
		DisableKeepAlives:   false,
		MaxIdleConns:        1024,
		MaxIdleConnsPerHost: 64, // Bursty concurrent forwards to one worker reuse pooled
		//                            libp2p streams instead of triggering gostream.Dial each time.
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Minute,
	}
	return rt
}

// RoundTrip implements http.RoundTripper.
func (rt *libp2pHTTPRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Scheme != "libp2p" {
		return nil, fmt.Errorf("libp2pHTTP: expected libp2p:// URL, got %v", req.URL)
	}
	cloned := req.Clone(req.Context())
	cloned.URL = &url.URL{
		Scheme:   "http",
		Host:     req.URL.Host,
		Path:     req.URL.Path,
		RawQuery: req.URL.RawQuery,
		Fragment: req.URL.Fragment,
	}
	cloned.Host = req.URL.Host
	return rt.inner.RoundTrip(cloned)
}

// CloseIdleConnections forwards to the inner transport.
func (rt *libp2pHTTPRoundTripper) CloseIdleConnections() {
	rt.inner.CloseIdleConnections()
}

package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

func newLibp2pHTTPRoundTripper(h host.Host) *libp2pHTTPRoundTripper {
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
			// Permit opening a stream over a relay-circuit (Limited) connection.
			// libp2p's NewStream refuses Limited conns by default and tries to
			// upgrade to direct, which on cross-cluster peers blocks until the
			// caller's context expires (manifests as plain "context deadline
			// exceeded" with no dial-failure prefix). The ping protocol opts in
			// the same way at p2p/protocol/ping/ping.go:117.
			dialCtx := network.WithAllowLimitedConn(ctx, "libp2p-http")
			return gostream.Dial(dialCtx, h, pid, p2phttp.DefaultP2PProtocol)
		},
		DisableKeepAlives:     false,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   4,
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

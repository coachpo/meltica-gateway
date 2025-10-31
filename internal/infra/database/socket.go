// Package database defines socket primitives for database integrations.
package database

import (
	"context"
	"net"
)

// SocketEndpoint describes a network endpoint accessible via sockets.
type SocketEndpoint interface {
	Network() string
	Address() string
}

// SocketDialer establishes socket connections to endpoints.
type SocketDialer interface {
	DialContext(ctx context.Context, endpoint SocketEndpoint) (net.Conn, error)
}

// SocketConnector provides high-level socket connection orchestration.
type SocketConnector interface {
	Connect(ctx context.Context) (net.Conn, error)
}

// SocketFactory produces dialers and connectors targeting a specific endpoint.
type SocketFactory interface {
	Endpoint() SocketEndpoint
	Dialer() SocketDialer
	Connector() SocketConnector
}

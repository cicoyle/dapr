// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// TODO: add modified by dapr add commit url from theirs (with commit hash)

package transport

import (
	"context"
	"crypto/tls"
	"net"

	"go.uber.org/zap"

	"github.com/dapr/dapr/pkg/security"
)

// NewListener creates a new listener.
func NewListener(addr, scheme string, tlsinfo *TLSInfo) (l net.Listener, err error) {
	return newListener(addr, scheme, WithTLSInfo(tlsinfo))
}

// NewListenerWithOpts creates a new listener which accepts listener options.
func NewListenerWithOpts(addr, scheme string, tlsinfo *TLSInfo, opts ...ListenerOption) (net.Listener, error) {
	allOpts := append([]ListenerOption{WithTLSInfo(tlsinfo)}, opts...)
	return newListener(addr, scheme, allOpts...)
}

func newListener(addr, scheme string, opts ...ListenerOption) (net.Listener, error) {
	if scheme == "unix" || scheme == "unixs" {
		// unix sockets via unix://laddr
		return NewUnixListener(addr)
	}

	lnOpts := newListenOpts(opts...)

	ln, err := newKeepAliveListener(nil, addr)
	if err != nil {
		return nil, err
	}
	lnOpts.Listener = ln

	return lnOpts.Listener, nil
}

func newKeepAliveListener(cfg *net.ListenConfig, addr string) (ln net.Listener, err error) {
	if cfg != nil {
		ln, err = cfg.Listen(context.TODO(), "tcp", addr)
	} else {
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return
	}

	return NewKeepAliveListener(ln, "tcp", nil)
}

type TLSInfo struct {
	// Logger logs TLS errors.
	// If nil, all logs are discarded.
	Logger *zap.Logger

	Security security.Handler

	// HandshakeFailure is optionally called when a connection fails to handshake. The
	// connection will be closed immediately afterward.
	HandshakeFailure func(*tls.Conn, error)
}

// ServerConfig generates a tls.Config object for use by an HTTP server.
func (info TLSInfo) ServerConfig() (*tls.Config, error) {
	cfg, err := info.Security.MTLSServerConfig(info.Security.ControlPlaneTrustDomain(), info.Security.ControlPlaneNamespace(), "dapr-scheduler")
	if err != nil {
		return nil, err
	}

	if info.Logger == nil {
		info.Logger = zap.NewNop()
	}

	cfg.ClientAuth = tls.RequireAndVerifyClientCert

	// "h2" NextProtos is necessary for enabling HTTP2 for go's HTTP server
	cfg.NextProtos = []string{"h2"}

	return cfg, nil
}

// ClientConfig generates a tls.Config object for use by an HTTP client.
func (info TLSInfo) ClientConfig() (*tls.Config, error) {
	cfg, err := info.Security.MTLSClientConfig(info.Security.ControlPlaneTrustDomain(), info.Security.ControlPlaneNamespace(), "dapr-scheduler")
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

package client

import (
	"context"
	"net"
	"time"

	grpcMiddleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpcRetry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"

	diag "github.com/dapr/dapr/pkg/diagnostics"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	"github.com/dapr/dapr/pkg/security"
)

const (
	dialTimeout = 1 * time.Second
)

// TODO: expand with streaming bits
type SchedulerClient struct {
	// getGrpcOpts are the options that should be used to connect to the scheduler service
	getGrpcOpts func() ([]grpc.DialOption, error)

	// Client is the gRPC client.
	Client schedulerv1pb.SchedulerClient

	// clientConn is the gRPC client connection.
	ClientConn *grpc.ClientConn
}

// NewSchedulerClient creates a new scheduler client for the given dial opts.
func NewSchedulerClient(optionGetter func() ([]grpc.DialOption, error)) (*SchedulerClient, error) {
	return &SchedulerClient{
		getGrpcOpts: optionGetter,
	}, nil
}

// addDNSResolverPrefix add the `dns://` prefix to the given addresses
func AddDNSResolverPrefix(addr []string) []string {
	resolvers := make([]string, 0, len(addr))
	for _, a := range addr {
		prefix := ""
		host, _, err := net.SplitHostPort(a)
		if err == nil && net.ParseIP(host) == nil {
			prefix = "dns:///"
		}
		resolvers = append(resolvers, prefix+a)
	}
	return resolvers
}

// GetSchedulerClient returns a new scheduler client and the underlying connection.
// If a cert chain is given, a TLS connection will be established.
func GetSchedulerClient(ctx context.Context, address string, sec security.Handler) (schedulerv1pb.SchedulerClient, *grpc.ClientConn, error) {
	unaryClientInterceptor := grpcRetry.UnaryClientInterceptor()

	if diag.DefaultGRPCMonitoring.IsEnabled() {
		unaryClientInterceptor = grpcMiddleware.ChainUnaryClient(
			unaryClientInterceptor,
			diag.DefaultGRPCMonitoring.UnaryClientInterceptor(),
		)
	}

	schedulerID, err := spiffeid.FromSegments(sec.ControlPlaneTrustDomain(), "ns", sec.ControlPlaneNamespace(), "dapr-scheduler")
	if err != nil {
		return nil, nil, err
	}

	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(unaryClientInterceptor),
		sec.GRPCDialOptionMTLS(schedulerID), grpc.WithReturnConnectionError(),
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	// check mode. dns resolution. create len clients based on len of results of dns res
	// handle re-est connections
	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, nil, err
	}
	return schedulerv1pb.NewSchedulerClient(conn), conn, nil
}

// ConnectToServer initializes a new connection to the target server
func (c *SchedulerClient) ConnectToServer(ctx context.Context, serverAddr string) error {
	opts, err := c.getGrpcOpts()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	// check mode. dns resolution. create len clients based on len of results of dns res
	// handle re-est connections
	conn, err := grpc.DialContext(ctx, serverAddr, opts...)
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		return err
	}

	c.ClientConn = conn
	c.Client = schedulerv1pb.NewSchedulerClient(conn)
	return nil
}

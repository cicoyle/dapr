/*
Copyright 2023 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	diag "github.com/dapr/dapr/pkg/diagnostics"
	"github.com/dapr/dapr/pkg/security"
	"github.com/google/martian/log"
	grpcRetry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"
	"strings"
	"sync"
)

const grpcServiceConfig = `{"loadBalancingPolicy":"round_robin"}`

// GetGrpcOptsGetter returns a function that provides the grpc options and once defined, a cached version will be returned.
func GetGrpcOptsGetter(servers []string, sec security.Handler) func() ([]grpc.DialOption, error) {
	mu := sync.RWMutex{}
	var cached []grpc.DialOption

	return func() ([]grpc.DialOption, error) {
		mu.RLock()
		if cached != nil {
			mu.RUnlock()
			return cached, nil
		}
		mu.RUnlock()
		mu.Lock()
		defer mu.Unlock()

		if cached != nil { // double check lock
			return cached, nil
		}

		schedulerID, err := spiffeid.FromSegments(sec.ControlPlaneTrustDomain(), "ns", sec.ControlPlaneNamespace(), "dapr-scheduler")
		if err != nil {
			log.Errorf("error establishing tls connection: %v", err)
			return nil, err
		}
		unaryClientInterceptor := grpcRetry.UnaryClientInterceptor()

		opts := []grpc.DialOption{
			grpc.WithUnaryInterceptor(unaryClientInterceptor),
			sec.GRPCDialOptionMTLS(schedulerID), grpc.WithReturnConnectionError(),
		}

		if diag.DefaultGRPCMonitoring.IsEnabled() {
			opts = append(
				opts,
				grpc.WithUnaryInterceptor(diag.DefaultGRPCMonitoring.UnaryClientInterceptor()),
			)
		}

		if len(servers) == 1 && strings.HasPrefix(servers[0], "dns:///") {
			// In Kubernetes environment, dapr-scheduler headless service resolves multiple IP addresses.
			// With round-robin load balancer.
			opts = append(opts, grpc.WithDefaultServiceConfig(grpcServiceConfig))
		}
		cached = opts
		return cached, nil
	}
}

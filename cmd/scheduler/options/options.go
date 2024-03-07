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

package options

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/dapr/dapr/pkg/metrics"
	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/security"
	securityConsts "github.com/dapr/dapr/pkg/security/consts"
	"github.com/dapr/kit/logger"
	"github.com/spf13/pflag"
)

type Options struct {
	Port             int
	HealthzPort      int
	ListenAddress    string
	PlacementAddress string
	TLSEnabled       bool
	TrustDomain      string
	TrustAnchorsFile string
	SentryAddress    string
	Mode             string

	EtcdDataDir string
	EtcdID      string
	//etcdPeerFlag     []string
	EtcdInitialPeers            string
	etcdAdvertisePeersURLsFlag  string
	EtcdAdvertisePeersURLs      []url.URL
	etcdAdvertiseClientURLsFlag string
	EtcdAdvertiseClientURLs     []url.URL
	etcdListenClientURLsFlag    string
	EtcdListenClientURLs        []url.URL
	etcdListenPeerURLsFlag      string
	EtcdListenPeerURLs          []url.URL

	Logger  logger.Options
	Metrics *metrics.Options
}

func New(origArgs []string) *Options {
	// We are using pflag to parse the CLI flags
	// pflag is a drop-in replacement for the standard library's "flag" package, however…
	// There's one key difference: with the stdlib's "flag" package, there are no short-hand options so options can be defined with a single slash (such as "daprd -mode").
	// With pflag, single slashes are reserved for shorthands.
	// So, we are doing this thing where we iterate through all args and double-up the slash if it's single
	// This works *as long as* we don't start using shorthand flags (which haven't been in use so far).
	args := make([]string, len(origArgs))
	for i, a := range origArgs {
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			args[i] = "-" + a
		} else {
			args[i] = a
		}
	}

	opts := Options{}

	// Create a flag set
	fs := pflag.NewFlagSet("sentry", pflag.ExitOnError)
	fs.SortFlags = true

	fs.IntVar(&opts.Port, "port", 50006, "The port for the scheduler server to listen on")
	fs.IntVar(&opts.HealthzPort, "healthz-port", 8080, "The port for the healthz server to listen on")

	fs.StringVar(&opts.ListenAddress, "listen-address", "", "The address for the Scheduler to listen on")
	fs.BoolVar(&opts.TLSEnabled, "tls-enabled", false, "Should TLS be enabled for the scheduler gRPC server")
	fs.StringVar(&opts.TrustDomain, "trust-domain", "localhost", "Trust domain for the Dapr control plane")
	fs.StringVar(&opts.TrustAnchorsFile, "trust-anchors-file", securityConsts.ControlPlaneDefaultTrustAnchorsPath, "Filepath to the trust anchors for the Dapr control plane")
	fs.StringVar(&opts.SentryAddress, "sentry-address", fmt.Sprintf("dapr-sentry.%s.svc:443", security.CurrentNamespace()), "Address of the Sentry service")
	fs.StringVar(&opts.PlacementAddress, "placement-address", "", "Addresses for Dapr Actor Placement service")
	fs.StringVar(&opts.Mode, "mode", string(modes.StandaloneMode), "Runtime mode for Dapr Scheduler")

	fs.StringVar(&opts.EtcdDataDir, "etcd-data-dir", "./data", "Directory to store scheduler etcd data")
	fs.StringVar(&opts.EtcdID, "id", "dapr-scheduler-0", "Scheduler server ID")
	//fs.StringSliceVar(&opts.etcdPeerFlag, "initial-cluster", []string{"dapr-scheduler-0=127.0.0.1:2379"}, "etcd cluster peers")
	fs.StringVar(&opts.EtcdInitialPeers, "initial-cluster", "dapr-scheduler-server-0=http://dapr-scheduler-server-0.dapr-system.svc:2380", "Initial etcd cluster peers")
	//fs.StringVar(&opts.EtcdAdvertisePeersURL, "initial-advertise-peer-urls", "http://dapr-scheduler-server-0.dapr-system.svc:2380", "URL that Etcd advertises to other nodes for peer-to-peer communication")
	fs.StringVar(&opts.etcdAdvertisePeersURLsFlag, "initial-advertise-peer-urls", "http://dapr-scheduler-server-0.dapr-system.svc:2380", "List of URLs that Etcd advertises to other nodes for peer-to-peer communication")
	fs.StringVar(&opts.etcdListenPeerURLsFlag, "listen-peer-urls", "http://dapr-scheduler-server-0.dapr-system.svc:2380", "List of URLs that Etcd listens on for peer-to-peer communication")
	fs.StringVar(&opts.etcdAdvertiseClientURLsFlag, "advertise-client-urls", "http://dapr-scheduler-server-0.dapr-system.svc:2379", "List of URLs that Etcd advertises to clients for communication") // TODO: confirm - do we want this? For debugging maybe?
	fs.StringVar(&opts.etcdListenClientURLsFlag, "listen-client-urls", "http://dapr-scheduler-server-0.dapr-system.svc:2379", "List of URLs that Etcd listens on for client communication")

	opts.Logger = logger.DefaultOptions()
	opts.Logger.AttachCmdFlags(fs.StringVar, fs.BoolVar)

	opts.Metrics = metrics.DefaultMetricOptions()
	opts.Metrics.AttachCmdFlags(fs.StringVar, fs.BoolVar)

	_ = fs.Parse(args)

	// TODO: handle err below
	opts.EtcdAdvertisePeersURLs, _ = parsePeersFromList(opts.etcdAdvertisePeersURLsFlag)
	opts.EtcdAdvertiseClientURLs, _ = parsePeersFromList(opts.etcdAdvertiseClientURLsFlag)
	opts.EtcdListenClientURLs, _ = parsePeersFromList(opts.etcdListenClientURLsFlag)
	opts.EtcdListenPeerURLs, _ = parsePeersFromList(opts.etcdListenPeerURLsFlag)
	//opts.EtcdPeers = parsePeersFromList(opts.etcdPeerFlag)

	return &opts
}

func parsePeersFromList(input string) ([]url.URL, error) {
	log.Println("CASSIE: input to parsePeersFromList: %s", input)
	var urlSlice []url.URL

	slice := strings.Split(input, ",")
	for _, s := range slice {
		trimmed := strings.TrimSpace(s)
		parsedURL, err := url.Parse(trimmed)
		if err != nil {
			return nil, err
		}
		urlSlice = append(urlSlice, *parsedURL)
	}
	log.Println("CASSIE: output from parsePeersFromList: %s", urlSlice)

	return urlSlice, nil
	//slice := strings.Split(input, ",")
	//
	//for i, s := range slice {
	//	slice[i] = strings.TrimSpace(s)
	//}
	//
	//return slice
}

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

package server

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"go.etcd.io/etcd/server/v3/embed"

	"github.com/dapr/dapr/pkg/modes"
	"github.com/dapr/dapr/pkg/security"
)

func (s *Server) conf() *embed.Config {
	config := embed.NewConfig()

	config.Name = s.id
	config.Dir = s.dataDir + "-" + security.CurrentNamespace() + "-" + s.id
	config.InitialCluster = strings.Join(s.etcdInitialPeers, ",")
	config.QuotaBackendBytes = s.etcdSpaceQuota
	config.AutoCompactionMode = s.etcdCompactionMode
	config.AutoCompactionRetention = s.etcdCompactionRetention

	etcdURL, peerPort, err := peerHostAndPort(s.id, s.etcdInitialPeers)
	if err != nil {
		log.Warnf("Invalid format for initial cluster. Make sure to include 'http://' in Scheduler URL")
	}

	config.AdvertisePeerUrls = []url.URL{{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%s", etcdURL, peerPort),
	}}

	config.AdvertiseClientUrls = []url.URL{{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%s", etcdURL, s.etcdClientPorts[s.id]),
	}}

	switch s.mode {
	// can't use domain name for k8s for config.ListenPeerUrls && config.ListenClientUrls
	case modes.KubernetesMode:
		etcdIP := "0.0.0.0"
		config.ListenPeerUrls = []url.URL{{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%s", etcdIP, peerPort),
		}}
		config.ListenClientUrls = []url.URL{{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%s", etcdIP, s.etcdClientPorts[s.id]),
		}}
		if len(s.etcdClientHttpPorts) > 0 {
			config.ListenClientHttpUrls = []url.URL{{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%s", etcdIP, s.etcdClientHttpPorts[s.id]),
			}}
		}
	default:
		config.ListenPeerUrls = []url.URL{{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%s", etcdURL, peerPort),
		}}
		config.ListenClientUrls = []url.URL{{
			Scheme: "http",
			Host:   fmt.Sprintf("%s:%s", etcdURL, s.etcdClientPorts[s.id]),
		}}
		if len(s.etcdClientHttpPorts) > 0 {
			config.ListenClientHttpUrls = []url.URL{{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%s", etcdURL, s.etcdClientHttpPorts[s.id]),
			}}
		}
	}

	config.LogLevel = "info" // Only supports debug, info, warn, error, panic, or fatal. Default 'info'.
	// TODO: Look into etcd config and if we need to do any raft compacting

	// TODO: Cassie do extra validation that the client port != peer port -> dont fail silently
	// TODO: Cassie do extra validation if people forget to put http:// -> dont fail silently
	// TODO: Cassie do extra validation to ensure that the list of ids sent in for the clientPort == list of ids from initial cluster

	return config
}

func peerHostAndPort(name string, initialCluster []string) (string, string, error) {
	for _, scheduler := range initialCluster {
		idAndAddress := strings.SplitN(scheduler, "=", 2)
		if len(idAndAddress) != 2 {
			log.Warnf("Incorrect format for initialPeerList: %s. Should contain <id>=http://<ip>:<peer-port>", initialCluster)
			continue
		}

		id := strings.TrimPrefix(idAndAddress[0], "http://")
		if id == name {
			address, err := url.Parse(idAndAddress[1])
			if err != nil {
				log.Warnf("Unable to parse url from initialPeerList: %s. Should contain <id>=http://<ip>:<peer-port>", initialCluster)
				continue
			}

			host, port, err := net.SplitHostPort(address.Host)
			if err != nil {
				return "", "", fmt.Errorf("error extracting port: %w", err)
			}

			return host, port, nil
		}
	}

	return "", "", fmt.Errorf("scheduler ID: %s is not found in initial cluster", name)
}

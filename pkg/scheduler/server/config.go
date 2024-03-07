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
	"go.etcd.io/etcd/server/v3/embed"
)

func (s *Server) conf() *embed.Config {
	config := embed.NewConfig()

	log.Debugf("\nCASSIE: SERVER config: %+v\n\n", s)
	//config.Name = "localhost"
	config.Name = s.etcdID //trying

	config.Dir = s.dataDir
	config.AdvertisePeerUrls = s.etcdAdvertisePeersURLs
	config.AdvertiseClientUrls = s.etcdAdvertiseClientURLs
	config.ListenClientUrls = s.etcdListenClientURLs
	config.ListenPeerUrls = s.etcdListenPeerURLs
	// AdvertisePeerUrls
	// TODO: pass value from CLI flag
	// config.LPUrls = parseEtcdUrls([]string{"http://0.0.0.0:2380"})
	// config.LCUrls = parseEtcdUrls([]string{"http://0.0.0.0:2379"})
	// config.APUrls = parseEtcdUrls([]string{"http://localhost:2380"})
	// config.ACUrls = parseEtcdUrls([]string{"http://localhost:2379"})
	//config.InitialCluster = "localhost=http://localhost:2380"
	config.InitialCluster = s.etcdInitialPeers

	config.LogLevel = "debug" // Only supports debug, info, warn, error, panic, or fatal. Default 'info'.
	log.Debugf("\nCASSIE: ETCD config: %+v\n\n", config)

	// TODO: Look into etcd config and if we need to do any raft compacting
	return config
}

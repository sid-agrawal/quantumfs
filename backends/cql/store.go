// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package cql

import (
	"fmt"
	"sync"
)

var scyllaUsername = "cql"
var scyllaPassword = "cql"

// Config struct holds the info needed to connect to a cql cluster
// and knobs for the different APIs
type Config struct {
	Cluster   ClusterConfig   `json:"cluster"`
	BlobStore BlobStoreConfig `json:"blobstore"`
	WsDB      WsDBConfig      `json:"wsdb"`
	BlobMap   BlobMapConfig   `json:"blobmap"`
}

// ClusterConfig struct holds the info needed to connect to a cql cluster
type ClusterConfig struct {
	Nodes              []string `json:"nodes"`
	ClusterName        string   `json:"clusterName"`
	NumConns           int      `json:"numconnections"`
	QueryNumRetries    int      `json:"querynumretries"`
	KeySpace           string   `json:"keyspace"`
	ConnTimeoutSec     int      `json:"conntimeoutsec"`
	Username           string   `json:"username"`
	Password           string   `json:"password"`
	CheckSchemaRetries int      `json:"checkschemaretries"`
}

// BlobStoreConfig holds config values specific to BlobStore API
type BlobStoreConfig struct {
	SomeConfig string `json:"someconfig"`
}

// DontExpireWsdbCache disables cache timeouts
const DontExpireWsdbCache = -1

// WsDBConfig holds config values specfic to WorkspaceDB API
type WsDBConfig struct {
	// CacheTimeoutSecs if set to DontExpireWsdbCache disables cache timeouts
	CacheTimeoutSecs int `json:"cachetimeoutsecs"`
}

// BlobMapConfig holds config values specific to BlobMap API
type BlobMapConfig struct {
	SomeConfig string `json:"someconfig"`
}

type cqlStoreGlobal struct {
	initMutex sync.RWMutex
	cluster   Cluster
	session   Session
	sem       Semaphore
}

var globalCqlStore cqlStoreGlobal

type cqlStore struct {
	cluster Cluster
	session Session

	sem *Semaphore
}

// Note: This routine is called by Init/New APIs
//       in Cql and only one global initialization is done.

// TBD: Need more investigation to see which parts of the
//      config can be dynamically updated
func initCqlStore(cluster Cluster) (cqlStore, error) {
	globalCqlStore.initMutex.Lock()
	defer globalCqlStore.initMutex.Unlock()

	if globalCqlStore.session == nil {
		session, err := cluster.CreateSession()
		if err != nil {
			err = fmt.Errorf("error in initCqlStore: %v", err)
			return cqlStore{}, err
		}
		// The semaphore limits the number of concurrent
		// inserts and queries to scyllaDB, otherwise we get timeouts
		// from ScyllaDB. Timeouts are unavoidable since its possible
		// to generate much faster rate of traffic than Scylla can handle.
		// The number 100, has been emperically determined.
		globalCqlStore.sem = make(Semaphore, 100)
		globalCqlStore.cluster = cluster
		globalCqlStore.session = session
	}

	var cStore cqlStore
	cStore.cluster = globalCqlStore.cluster
	cStore.session = globalCqlStore.session
	cStore.sem = &globalCqlStore.sem

	return cStore, nil
}

// mostly used by tests
func resetCqlStore() {
	globalCqlStore.initMutex.Lock()
	defer globalCqlStore.initMutex.Unlock()
	if globalCqlStore.session != nil {
		globalCqlStore.session.Close()
	}
	globalCqlStore.cluster = nil
	globalCqlStore.session = nil
	globalCqlStore.sem = nil
}

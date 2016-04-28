// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// Central configuration object and handling

package daemon

import "arista.com/quantumfs"

type QuantumFsConfig struct {
	CachePath string
	CacheSize uint64
	MountPath string

	// How long the kernel is allowed to cache values
	CacheTimeSeconds uint64
	CacheTimeNsecs   uint32

	WorkspaceDB  quantumfs.WorkspaceDB
	DurableStore quantumfs.DataStore
}
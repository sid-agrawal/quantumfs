// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package backends

import "github.com/aristanetworks/quantumfs/backends/systemlocal"

func init() {
	registerDatastore("systemlocal", systemlocal.NewDataStore)
	registerWorkspaceDB("systemlocal", systemlocal.NewWorkspaceDB)
}

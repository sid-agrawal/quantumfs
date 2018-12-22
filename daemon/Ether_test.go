// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Smoke tests for the Ether datastores

import (
	"testing"
)

func TestSmokeTestEtherFilesystem(t *testing.T) {
	runTestNoQfsExpensiveTest(t, func(test *testHelper) {
		test.startQuantumFs(test.etherFilesystemConfig(), nil, false)
		interDirectoryRename(test)
	})
}

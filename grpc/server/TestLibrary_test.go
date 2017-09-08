// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package server

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/grpc"
	"github.com/aristanetworks/quantumfs/qlog"
	"github.com/aristanetworks/quantumfs/testutils"
	"github.com/aristanetworks/quantumfs/thirdparty_backends"
	"github.com/aristanetworks/quantumfs/utils"
)

var serversLock utils.DeferableMutex
var servers map[uint16]*Server

func init() {
	servers = map[uint16]*Server{}
}

const initialPort = uint16(22222)

func runTest(t *testing.T, test serverTest) {
	runTestCommon(t, test, false)
}

// Run a server where the backend is erased when the server dies
func runTestWithEphemeralBackend(t *testing.T, test serverTest) {
	runTestCommon(t, test, true)
}

func runTestCommon(t *testing.T, test serverTest, ephemeral bool) {
	t.Parallel()

	// the stack depth of test name for all callers of runTest
	// is 2. Since the stack looks as follows:
	// 2 <testname>
	// 1 runTest
	// 0 runTestCommon
	testName := testutils.TestName(2)

	th := &testHelper{
		TestHelper: testutils.NewTestHelper(testName,
			testutils.TestRunDir, t),
	}
	th.ctx = newCtx(th.Logger)

	if ephemeral {
		th.backendType = "processlocal"
		th.backendConfig = ""
	} else {
		th.backendType = "systemlocal"
		th.backendConfig = th.TempDir + "/workspacedb"
	}

	func() {
		defer serversLock.Lock().Unlock()
		port := initialPort
		for {
			if _, used := servers[port]; !used {
				break
			}
			port++
		}

		th.Log("Starting server with path %s", th.backendConfig)
		server, err := StartWorkspaceDbd(th.Logger, port, th.backendType,
			th.backendConfig)
		if err != nil {
			t.Fatalf(fmt.Sprintf("Failed to initialize wsdb server: %s",
				err.Error()))
		}

		servers[port] = server
		th.server = server
		th.port = port

		if !ephemeral {
			th.backend = server.backend
		}
	}()

	defer th.EndTest()

	th.RunTestCommonEpilog(testName, th.testHelperUpcast(test))
}

type testHelper struct {
	testutils.TestHelper
	ctx           *quantumfs.Ctx
	server        *Server
	port          uint16
	backendType   string
	backendConfig string
	backend       quantumfs.WorkspaceDB
}

type serverTest func(test *testHelper)

func newCtx(logger *qlog.Qlog) *quantumfs.Ctx {
	// Create Ctx with random RequestId
	Qlog := logger
	requestId := qlog.TestReqId
	ctx := &quantumfs.Ctx{
		Qlog:      Qlog,
		RequestId: requestId,
	}

	return ctx
}

func (th *testHelper) testHelperUpcast(
	testFn func(test *testHelper)) testutils.QuantumFsTest {

	return func(test testutils.TestArg) {
		testFn(th)
	}
}

func (th *testHelper) stopServer() {
	if th.server != nil {
		err := th.server.Stop()
		if err != nil && !strings.Contains(err.Error(),
			"use of closed network connection") {

			th.AssertNoErr(err)
		}
	}
	th.server = nil
}

func (th *testHelper) EndTest() {
	th.stopServer()

	func() {
		defer serversLock.Lock().Unlock()
		delete(servers, th.port)
	}()

	th.TestHelper.EndTest()
}

func TestMain(m *testing.M) {
	flag.Parse()

	testutils.PreTestRuns()
	result := m.Run()
	testutils.PostTestRuns()

	os.Exit(result)
}

func (th *testHelper) newClient() quantumfs.WorkspaceDB {
	config := fmt.Sprintf("[::1]:%d", th.port)
	client := grpc.NewWorkspaceDB(config)

	return client
}

func (th *testHelper) restartServer() {
	backend := th.backend
	if backend == nil {
		wsdb, err := thirdparty_backends.ConnectWorkspaceDB(
			th.backendType, th.backendConfig)
		th.AssertNoErr(err)
		backend = wsdb
	}

	th.stopServer()

	defer serversLock.Lock().Unlock()
	th.Log("Starting server with path %s", th.backendConfig)
	server, err := startWorkspaceDbdWithBackend(th.Logger, th.port, backend)
	th.AssertNoErr(err)

	th.server = server
	servers[th.port] = th.server
}
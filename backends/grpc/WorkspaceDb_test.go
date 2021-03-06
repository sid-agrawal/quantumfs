// Copyright (c) 2018 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package grpc

// Test the workspaceDB implementation in grpc

import (
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/backends/grpc/rpc"
	"github.com/aristanetworks/quantumfs/utils"
)

func (test *testHelper) setupWsdb() (quantumfs.WorkspaceDB, *quantumfs.Ctx, *uint32,
	*testWorkspaceDbClient) {

	var serverDown uint32

	testWsdb := testWorkspaceDbClient{
		commandDelay: time.Millisecond * 1,
		logger:       test.Logger,
		stream:       newTestStream(),
		serverDown:   &serverDown,
	}

	wsdb := newWorkspaceDB_("", func(*grpc.ClientConn,
		string) (*grpc.ClientConn, rpc.WorkspaceDbClient) {

		return nil, &testWsdb
	})
	ctx := &quantumfs.Ctx{
		Qlog: test.Logger,
	}

	return wsdb, ctx, &serverDown, &testWsdb
}

func TestFetchWorkspace(t *testing.T) {
	runTest(t, func(test *testHelper) {
		wsdb, ctx, _, _ := test.setupWsdb()

		key, nonce, err := wsdb.Workspace(ctx, "a", "b", "c")
		test.Assert(key.IsEqualTo(quantumfs.EmptyWorkspaceKey),
			"incorrect wsr: %s vs %s", key.String(),
			quantumfs.EmptyWorkspaceKey.String())
		test.Assert(nonce.Id == 12345, "Wrong nonce %d", nonce.Id)
		test.AssertNoErr(err)
	})
}

func TestUpdateWorkspace(t *testing.T) {
	runTest(t, func(test *testHelper) {
		wsdb, _, _, rawWsdb := test.setupWsdb()

		numWorkspaces := 15000
		var workspaceLock utils.DeferableMutex
		workspaces := make(map[string]int)
		// Add some subscriptions
		for i := 0; i < numWorkspaces; i++ {
			newWorkspace := "test/test/" + strconv.Itoa(i)
			workspaces[newWorkspace] = 0
			wsdb.SubscribeTo(newWorkspace)
		}

		wsdb.SetCallback(func(updates map[string]quantumfs.WorkspaceState) {
			defer workspaceLock.Lock().Unlock()
			for wsrStr, _ := range updates {
				workspaces[wsrStr]++
			}
		})

		wsrStrs := make([]string, len(workspaces))
		for wsrStr, _ := range workspaces {
			wsrStrs = append(wsrStrs, wsrStr)
		}

		// Send in updates for every workspace
		for _, wsrStr := range wsrStrs {
			rawWsdb.sendNotification(wsrStr)
		}

		test.WaitFor("All workspaces to get notifications", func() bool {
			defer workspaceLock.Lock().Unlock()
			for _, count := range workspaces {
				if count == 0 {
					return false
				}
			}

			return true
		})
	})
}

func TestDisconnectedWorkspaceDB(t *testing.T) {
	runTest(t, func(test *testHelper) {
		wsdb, ctx, serverDown, rawWsdb := test.setupWsdb()

		numWorkspaces := 1000
		var workspaceLock utils.DeferableMutex
		workspaces := make(map[string]int)
		// Add some subscriptions
		for i := 0; i < numWorkspaces; i++ {
			newWorkspace := "test/test/" + strconv.Itoa(i)
			workspaces[newWorkspace] = 0

			wsdb.SubscribeTo(newWorkspace)
		}

		wsdb.SetCallback(func(updates map[string]quantumfs.WorkspaceState) {
			defer workspaceLock.Lock().Unlock()
			for wsrStr, _ := range updates {
				workspaces[wsrStr]++
			}
		})

		// Break the connection
		atomic.StoreUint32(serverDown, serverDownHang)

		// Cause the current stream to error out, indicating connection prob
		rawWsdb.stream.data <- nil

		commandsToFail := 1000
		// Queue up >1000 commands which should now fail with errors
		for i := 0; i < commandsToFail+100; i++ {
			go func() {
				defer func() {
					// suppress any panics due to reconnect()
					recover()
				}()

				_, _, err := wsdb.Workspace(ctx, "a", "b", "c")
				test.AssertErr(err)
			}()
		}

		// Wait for many failures to happen and potentially clog things
		// when fetchWorkspace repeatedly fails
		test.WaitForNLogStrings("Received grpc error", commandsToFail,
			"fetch errors to accumulate")

		// Bring the connection back
		atomic.StoreUint32(serverDown, serverDownReset)

		// Perform some basic operations which need the wsdb.lock -
		// if the lock is hung, then this test will timeout
		wsdb.SubscribeTo("post/test/wsrA")
		wsdb.SubscribeTo("post/test/wsrB")
		wsdb.SubscribeTo("post/test/wsrC")
	})
}

func TestRetries(t *testing.T) {
	runTest(t, func(test *testHelper) {
		wsdb, ctx, serverDown, _ := test.setupWsdb()

		// Break the connection hard, so all requests just fail
		atomic.StoreUint32(serverDown, 2)

		// Issue a fetch in parallel, which should retry loop
		fetchFinished := make(chan error)
		go func() {
			_, _, err := wsdb.Workspace(ctx, "a", "b", "c")
			fetchFinished <- err
		}()

		test.WaitForNLogStrings("failed, retrying", 5,
			"fetch to attempt retry a few times")

		// Unbreak the connection
		atomic.StoreUint32(serverDown, 0)

		// Ensure we check that the fetch eventually succeeds
		err := <-fetchFinished
		test.AssertNoErr(err)
	})
}

func TestUpdaterDoesntRetry(t *testing.T) {
	runTest(t, func(test *testHelper) {
		wsdb, ctx, serverDown, rawWsdb := test.setupWsdb()

		// Fetch a workspace once to link the qlog
		wsdb.Workspace(ctx, "a", "b", "c")

		// Subscribe to at least one workspace so that fetch is called
		// when we error out the stream
		err := wsdb.SubscribeTo("test/test/test")
		test.AssertNoErr(err)

		retriesBefore := test.CountLogStrings("failed, retrying")

		// Break the connection hard, so all requests fail not hang
		atomic.StoreUint32(serverDown, 2)

		// Cause reconnector to try reconnecting by making the stream
		// error out
		rawWsdb.stream.data <- nil

		// Wait for a few reconnector attempts to have happened
		test.WaitForNLogStrings(updateLog, 5, "reconnection to be attempted")

		// Restore the connect so we stop getting log spam
		atomic.StoreUint32(serverDown, 0)

		// Ensure that we never retried
		retriesAfter := test.CountLogStrings("failed, retrying")

		test.Assert(retriesAfter == retriesBefore,
			"stream reconnection invoked retrying")
	})
}

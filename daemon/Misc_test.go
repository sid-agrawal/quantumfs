// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test cases which do not belong in other test files

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/aristanetworks/quantumfs/testutils"
)

func TestUnknownInodeId(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()
		filename := workspace + "/file"

		// We need to create enough files that we can read
		// some from the directory without reading the entire
		// directory. Then we can cause a directory snapshot
		// to be taken, delete the file filename and then
		// continue reading. This will result in the inodeId
		// for the filename being returned to the kernel after
		// that ID is no longer valid. This entry will be
		// cached in the kernel and the subsequent open call
		// will cause an inode number entirely unknown to
		// QuantumFS to be used in QuantumFs.Open().
		for i := 0; i < 300; i++ {
			file := fmt.Sprintf("%s/filler-%d", workspace, i)
			test.AssertNoErr(testutils.PrintToFile(file, "contents"))
		}
		test.AssertNoErr(testutils.PrintToFile(filename, "contents"))
		inodeNum := test.getInodeNum(filename)

		dir, err := os.Open(workspace)
		test.AssertNoErr(err)
		defer dir.Close()
		_, err = dir.Readdir(10)
		test.AssertNoErr(err)

		test.AssertNoErr(syscall.Unlink(filename))

		test.SyncAllWorkspaces()
		test.qfs.Forget(uint64(inodeNum), 1)

		_, err = dir.Readdir(0)
		test.AssertNoErr(err)

		_, err = os.Open(filename)
		test.AssertErr(err)
		test.Assert(os.IsNotExist(err),
			"Expected ENOENT, got %s", err.Error())
	})
}

func TestDualInstances(t *testing.T) {
	runDualQuantumFsTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()
		filename := workspace + "/file"

		expectedData := test.MakeFile(filename)
		test.SyncAllWorkspaces()

		path := test.qfsInstances[1].config.MountPath + "/" +
			test.RelPath(filename)

		test.CheckData(path, expectedData)
	})
}

func TestWorkspacePubSubCallback(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()
		filename := workspace + "/file"

		test.MakeFile(filename)
		test.SyncAllWorkspaces()

		test.WaitForLogString("Mux::handleWorkspaceChanges",
			"Workspace pubsub callback to be called")
	})
}

func newInodeId(c *ctx, ids *inodeIds) InodeId {
	newId, _ := ids.newInodeId(c)
	return newId
}

func TestInodeIdsIncrementing(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		ids := newInodeIds(100*time.Millisecond, time.Hour)
		c := test.newCtx()
		test.Assert(newInodeId(c, ids) == 4, "Wrong 1st inodeId given")
		test.Assert(newInodeId(c, ids) == 5, "Wrong 2nd inodeId given")
		test.Assert(newInodeId(c, ids) == 6, "Wrong 3rd inodeId given")

		ids.releaseInodeId(c, 4)
		time.Sleep(50 * time.Millisecond)
		test.Assert(newInodeId(c, ids) == 7, "Wrong next id during delay")
		time.Sleep(60 * time.Millisecond)

		test.Assert(newInodeId(c, ids) == 4, "Didn't get to reuse 1st id")
		test.Assert(newInodeId(c, ids) == 8, "Wrong next id")
	})
}

func TestInodeIdsGarbageCollection(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		ids := newInodeIds(time.Millisecond, 100*time.Millisecond)
		c := test.newCtx()

		allocated := make([]InodeId, 100, 100)
		for i := 0; i < 100; i++ {
			allocated[i] = newInodeId(c, ids)
		}

		for i := 0; i < 100; i++ {
			ids.releaseInodeId(c, allocated[i])
		}

		time.Sleep(time.Millisecond * 10)

		func() {
			defer ids.lock.Lock().Unlock()
			test.Assert(ids.highMark == 104,
				"Garbage collection happened too soon")
		}()

		time.Sleep(time.Millisecond * 100)

		// go through all the ids and ensure that we garbage collected
		for i := 0; i < 90; i++ {
			newInodeId(c, ids)
		}

		func() {
			defer ids.lock.Lock().Unlock()
			test.Assert(ids.highMark == 94,
				"Garbage collection happened too soon")
		}()

		test.Assert(newInodeId(c, ids) == 94,
			"inodeIds didn't resume counting after GC")
	})
}

func TestInodeIdsReuseCheck(t *testing.T) {
	runTestCustomConfig(t, dirtyDelay100Ms, func(test *testHelper) {
		workspace := test.NewWorkspace()

		func() {
			defer test.qfs.inodeIds.lock.Lock().Unlock()
			test.qfs.inodeIds.reusableDelay = time.Millisecond * 2
			test.qfs.inodeIds.gcPeriod = time.Millisecond * 20
		}()

		test.AssertNoErr(os.MkdirAll(workspace+"/dirA/dirB", 0777))
		test.MakeFile(workspace + "/dirA/dirB/fileA")

		test.AssertNoErr(os.MkdirAll(workspace+"/dirA/dirC", 0777))
		test.MakeFile(workspace + "/dirA/dirC/fileB")

		fileA := test.getInodeNum(workspace + "/dirA/dirB/fileA")
		dirC := test.getInodeNum(workspace + "/dirA/dirC")
		fileB := test.getInodeNum(workspace + "/dirA/dirC/fileB")
		test.Assert(dirC == fileA+1, "inode id not simply incremented")
		test.Assert(fileB == dirC+1, "inode id not simply incremented")

		c := test.newCtx()
		// wait for garbage collection to happen at least once
		test.WaitFor("inode ids to be garbage collected", func() bool {
			defer test.qfs.inodeIds.lock.Lock().Unlock()
			test.qfs.inodeIds.testHighmark_(c)
			return test.qfs.inodeIds.highMark < uint64(fileB)
		})

		test.MakeFile(workspace + "/dirA/fileC")
		test.MakeFile(workspace + "/dirA/fileD")
		fileC := test.getInodeNum(workspace + "/dirA/fileC")
		fileD := test.getInodeNum(workspace + "/dirA/fileD")
		test.Assert(fileC == fileB+1, "inode id not incremented after GC")
		test.Assert(fileD == fileC+1, "inode id not incremented after GC")
	})
}

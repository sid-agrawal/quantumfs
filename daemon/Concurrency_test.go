// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test concurrent workspaces

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"syscall"
	"testing"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/testutils"
)

func (test *testHelper) setupDual() (workspace0 string, workspace1 string) {
	workspace0 = test.NewWorkspace()
	mnt1 := test.qfsInstances[1].config.MountPath
	workspaceName := test.RelPath(workspace0)
	workspace1 = mnt1 + "/" + workspaceName

	api1, err := quantumfs.NewApiWithPath(mnt1 + "/api")
	test.AssertNoErr(err)
	defer api1.Close()

	test.AssertNoErr(api1.EnableRootWrite(workspaceName))

	return workspace0, workspace1
}

// Specify data of length zero to wait for file to not exist
func (test *testHelper) waitForPropagate(file string, data []byte) {
	test.WaitFor(file+" to propagate", func() bool {
		fd, err := os.Open(file)
		defer fd.Close()
		if len(data) == 0 {
			return err != os.ErrNotExist
		}

		readData, err := ioutil.ReadFile(file)
		if err != nil {
			return false
		}

		if !bytes.Equal(readData, data) {
			test.qfs.c.vlog("Propagation %s vs %s", readData, data)
			return false
		}

		return true
	})
}

func TestConcurrentReadWrite(t *testing.T) {
	runDualQuantumFsTest(t, func(test *testHelper) {
		workspace0, workspace1 := test.setupDual()

		dataA := []byte("abc")
		dataB := []byte("def")
		fileA := "/fileA"
		fileB := "/fileB"

		test.AssertNoErr(testutils.PrintToFile(workspace0+fileA,
			string(dataA)))

		test.waitForPropagate(workspace1+fileA, dataA)

		test.AssertNoErr(testutils.PrintToFile(workspace0+fileB,
			string(dataA)))
		test.AssertNoErr(testutils.PrintToFile(workspace1+fileB,
			string(dataB)))

		test.waitForPropagate(workspace0+fileB, dataB)
	})
}

func TestConcurrentWriteDeletion(t *testing.T) {
	runDualQuantumFsTest(t, func(test *testHelper) {
		workspace0, workspace1 := test.setupDual()
		dataA := []byte("abc")
		dataB := []byte("def")
		dataC := []byte("ghijk")
		file := "/fileA"

		// Open a file handle to be orphaned and write some data
		fd, err := os.OpenFile(workspace0+file, os.O_RDWR|os.O_CREATE, 0777)
		defer fd.Close()
		test.AssertNoErr(err)
		n, err := fd.Write(dataA)
		test.Assert(n == len(dataA), "Not all data written")
		test.AssertNoErr(err)

		test.waitForPropagate(workspace1+file, dataA)

		// Orphan the file from the other workspace
		os.Remove(workspace1 + file)

		// Wait for file to be deleted
		test.waitForPropagate(workspace0+file, []byte{})

		// Check that our file was orphaned
		n, err = fd.Write(dataB)
		test.Assert(n == len(dataB), "Not all dataB written")
		test.AssertNoErr(err)

		// Now check that we can make a new file in its place with the orphan
		// still around
		test.AssertNoErr(testutils.PrintToFile(workspace1+file,
			string(dataC)))

		test.waitForPropagate(workspace0+file, dataC)

		// Check that we can still read everything from the orphan
		buf := make([]byte, 10)
		n, err = fd.ReadAt(buf, 0)
		test.Assert(err == io.EOF, "Didn't read all file contents")
		test.Assert(bytes.Equal(buf[:n], append(dataA, dataB...)),
			"Mismatched data in orphan: %s", buf[:n])
	})
}

func TestConcurrentHardlinks(t *testing.T) {
	runDualQuantumFsTest(t, func(test *testHelper) {
		workspace0, workspace1 := test.setupDual()

		dataA := []byte("abc")
		fileA := "/fileA"
		fileB := "/fileB"

		test.AssertNoErr(testutils.PrintToFile(workspace0+fileA,
			string(dataA)))

		test.waitForPropagate(workspace1+fileA, dataA)

		test.AssertNoErr(syscall.Link(workspace1+fileA, workspace1+fileB))

		test.waitForPropagate(workspace0+fileB, dataA)

		test.AssertNoErr(os.Remove(workspace0 + fileA))

		test.waitForPropagate(workspace1+fileA, []byte{})
	})
}

func TestConcurrentIntraFileMerges(t *testing.T) {
	runDualQuantumFsTest(t, func(test *testHelper) {
		workspace0, workspace1 := test.setupDual()

		dataA := []byte("0000\n00\n0000")
		dataB := []byte("0000\n22\n0444")
		dataC := []byte("1110\n33\n0000")
		expect := []byte("1110\n33\n0444")
		file := "/file"

		test.AssertNoErr(testutils.PrintToFile(workspace0+file,
			string(dataA)))

		test.waitForPropagate(workspace1+file, dataA)

		test.AssertNoErr(testutils.OverWriteFile(workspace1+file,
			string(dataB)))

		test.AssertNoErr(testutils.OverWriteFile(workspace0+file,
			string(dataC)))

		// There should be a merge conflict, resolved by an intra-file
		// merge, that eventually is reflected in both workspaces
		test.waitForPropagate(workspace0+file, expect)
		test.waitForPropagate(workspace1+file, expect)
	})
}

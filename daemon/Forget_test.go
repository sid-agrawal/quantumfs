// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test that inodes can be Forgotten and re-accessed

import "bytes"
import "io/ioutil"
import "os"
import "os/exec"
import "strconv"
import "strings"
import "testing"

import "github.com/aristanetworks/quantumfs/qlog"

func TestForgetOnDirectory(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.newWorkspace()
		os.MkdirAll(workspace+"/dir", 0777)

		numFiles := 10
		data := genData(255)
		// Generate a bunch of files
		for i := 0; i < numFiles; i++ {
			err := printToFile(workspace+"/dir/file"+strconv.Itoa(i),
				string(data))
			test.assert(err == nil, "Error creating small file")
		}

		// Now force the kernel to drop all cached inodes
		cmd := exec.Command("mount", "-i", "-oremount", test.tempDir+
			"/mnt")
		errorStr, err := cmd.CombinedOutput()
		test.assert(err == nil, "Unable to force vfs to drop dentry cache")
		test.assert(len(errorStr) == 0, "Error during remount: %s", errorStr)

		logFile := test.tempDir + "/ramfs/qlog"
		logOutput := qlog.ParseLogs(logFile)
		test.assert(strings.Contains(logOutput, "Forgetting"),
			"No inode forget triggered during dentry drop.")

		// Now read all the files back to make sure we still can
		for i := 0; i < numFiles; i++ {
			var readBack []byte
			readBack, err := ioutil.ReadFile(workspace + "/dir/file" +
				strconv.Itoa(i))
			test.assert(bytes.Equal(readBack, data),
				"File contents not preserved after Forget")
			test.assert(err == nil, "Unable to read file after Forget")
		}
	})
}

func TestForgetOnWorkspaceRoot(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.newWorkspace()

		numFiles := 10
		data := genData(255)
		// Generate a bunch of files
		for i := 0; i < numFiles; i++ {
			err := printToFile(workspace+"/file"+strconv.Itoa(i),
				string(data))
			test.assert(err == nil, "Error creating small file")
		}

		// Now force the kernel to drop all cached inodes
		cmd := exec.Command("mount", "-i", "-oremount", test.tempDir+
			"/mnt")
		errorStr, err := cmd.CombinedOutput()
		test.assert(err == nil, "Unable to force vfs to drop dentry cache")
		test.assert(len(errorStr) == 0, "Error during remount: %s", errorStr)

		logFile := test.tempDir + "/ramfs/qlog"
		logOutput := qlog.ParseLogs(logFile)
		test.assert(strings.Contains(logOutput, "Forgetting"),
			"No inode forget triggered during dentry drop.")

		// Now read all the files back to make sure we still can
		for i := 0; i < numFiles; i++ {
			var readBack []byte
			readBack, err := ioutil.ReadFile(workspace + "/file" +
				strconv.Itoa(i))
			test.assert(bytes.Equal(readBack, data),
				"File contents not preserved after Forget")
			test.assert(err == nil, "Unable to read file after Forget")
		}
	})
}

func TestForgetUninstantiatedChildren(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.newWorkspace()
		dirName := workspace + "/dir"

		err := os.Mkdir(dirName, 0777)
		test.assert(err == nil, "Failed creating directory: %v", err)

		numFiles := 10
		data := genData(255)
		// Generate a bunch of files
		for i := 0; i < numFiles; i++ {
			err := printToFile(workspace+"/dir//file"+strconv.Itoa(i),
				string(data))
			test.assert(err == nil, "Error creating small file")
		}

		// Now branch this workspace so we have a workspace full of
		// uninstantiated Inodes
		workspace = test.branchWorkspace(workspace)
		dirName = test.absPath(workspace + "/dir")

		// Get the listing from the directory to instantiate that directory
		// and add its children to the uninstantiated inode list.
		dir, err := os.Open(dirName)
		test.assert(err == nil, "Error opening directory: %v", err)
		children, err := dir.Readdirnames(-1)
		test.assert(err == nil, "Error reading directory children: %v", err)
		test.assert(len(children) == numFiles+2,
			"Wrong number of children: %d != %d", len(children),
			numFiles+2)
		dir.Close()

		numUninstantiatedOld := len(test.qfs.uninstantiatedInodes)

		// Forgetting should now forget the Directory and thus remove all the
		// uninstantiated children from the uninstantiatedInodes list.
		cmd := exec.Command("mount", "-i", "-oremount", test.tempDir+
			"/mnt")
		errorStr, err := cmd.CombinedOutput()
		test.assert(err == nil, "Unable to force vfs to drop dentry cache")
		test.assert(len(errorStr) == 0, "Error during remount: %s", errorStr)

		logFile := test.tempDir + "/ramfs/qlog"
		logOutput := qlog.ParseLogs(logFile)
		test.assert(strings.Contains(logOutput, "Forgetting"),
			"No inode forget triggered during dentry drop.")

		numUninstantiatedNew := len(test.qfs.uninstantiatedInodes)

		test.assert(numUninstantiatedOld > numUninstantiatedNew,
			"No uninstantiated inodes were removed")
	})
}

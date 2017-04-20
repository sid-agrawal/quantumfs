// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test workspace merging

import "os"
import "syscall"
import "testing"


func TestBasicMerge(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspaceA := test.NewWorkspace()
		workspaceB := test.NewWorkspace()

		fileA := workspaceA + "/fileA"
		fileB := workspaceB + "/fileB"
		fileC := workspaceA + "/subdir/fileC"
		fileC2 := workspaceB + "/subdir/fileC"
		fileD := workspaceA + "/subdir/fileD"
		fileD2 := workspaceB + "/subdir/fileD"

		err := os.MkdirAll(workspaceA + "/subdir", 0777)
		test.AssertNoErr(err)
		err = os.MkdirAll(workspaceB + "/subdir", 0777)
		test.AssertNoErr(err)

		dataA := test.MakeFile(fileA)
		dataB := test.MakeFile(fileB)
		test.MakeFile(fileC)
		dataC2 := test.MakeFile(fileC2)
		// reverse the order for the D files
		test.MakeFile(fileD2)
		dataD := test.MakeFile(fileD)

		test.SyncAllWorkspaces()

		api := test.getApi()
		err = api.Merge(test.RelPath(workspaceB), test.RelPath(workspaceA))
		test.AssertNoErr(err)

		test.CheckData(fileA, dataA)
		test.CheckData(workspaceA + "/fileB", dataB)
		// ensure we took remote
		test.CheckData(fileC, dataC2)
		// ensure we took local
		test.CheckData(fileD, dataD)
	})
}

func TestSpecialsMerge(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspaceA := test.NewWorkspace()
		workspaceB := test.NewWorkspace()

		fileA := workspaceA + "/fileA"
		fileB := workspaceB + "/fileB"
		symlinkA := workspaceA + "/symlink"
		symlinkB := workspaceB + "/symlink"
		symlinkB2 := workspaceB + "/symlink2"
		symlinkA2 := workspaceA + "/symlink2"
		specialA := workspaceA + "/special"
		specialB := workspaceB + "/special"

		dataA := test.MakeFile(fileA)
		dataB := test.MakeFile(fileB)
		err := syscall.Symlink(fileA, symlinkA)
		test.AssertNoErr(err)
		err = syscall.Symlink(fileB, symlinkB)
		test.AssertNoErr(err)
		err = syscall.Symlink(fileB, symlinkB2)
		test.AssertNoErr(err)
		err = syscall.Symlink(fileA, symlinkA2)
		test.AssertNoErr(err)

		err = syscall.Mknod(specialA, syscall.S_IFCHR, 0x12345678)
		test.AssertNoErr(err)
		err = syscall.Mknod(specialB, syscall.S_IFBLK, 0x11111111)
		test.AssertNoErr(err)

		test.SyncAllWorkspaces()

		api := test.getApi()
		err = api.Merge(test.RelPath(workspaceB), test.RelPath(workspaceA))
		test.AssertNoErr(err)

		test.CheckData(fileA, dataA)
		// symlink should be overwritten and pointing to fileB
		test.CheckData(symlinkA, dataB)
		test.CheckData(workspaceA + "/fileB", dataB)
		// remote should have been overwritten this time
		test.CheckData(symlinkA2, dataA)

		// Check that the remote special was taken
		var stat syscall.Stat_t
		err = syscall.Stat(specialA, &stat)
		err = syscall.Stat(specialA, &stat)
		test.AssertNoErr(err)
		test.Assert(stat.Rdev == 0x11111111,
			"remote didn't overwrite local %x", stat.Rdev)
		test.Assert(stat.Mode == syscall.S_IFBLK, "remote mode mismatch")
	})
}

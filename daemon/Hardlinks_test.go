// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test that different parts of Hardlink support are working

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"testing"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/testutils"
	"github.com/aristanetworks/quantumfs/utils"
)

func TestHardlinkReload(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()
		err := utils.MkdirAll(workspace+"/subdir/grandchild", 0777)
		test.AssertNoErr(err)

		// Create a couple files so we can copy its directory record
		data := GenData(2000)
		testFileA := workspace + "/subdir/testFile"
		err = testutils.PrintToFile(testFileA, string(data[:1000]))
		test.AssertNoErr(err)

		testFileB := workspace + "/subdir/testFileB"
		err = testutils.PrintToFile(testFileB, string(data))
		test.AssertNoErr(err)

		// artificially insert some hardlinks into the map
		wsr, cleanup := test.GetWorkspaceRoot(workspace)
		defer cleanup()

		err = syscall.Link(testFileA, workspace+"/subdir/linkFileA")
		test.AssertNoErr(err)
		err = syscall.Link(testFileA,
			workspace+"/subdir/grandchild/linkFileA2")
		test.AssertNoErr(err)
		err = syscall.Link(testFileB, workspace+"/linkFileB")
		test.AssertNoErr(err)

		// Write data to the hardlink to ensure its syncChild function works
		err = testutils.PrintToFile(
			workspace+"/subdir/grandchild/linkFileA2",
			string(data[1000:]))
		test.AssertNoErr(err)

		var nstat syscall.Stat_t
		err = syscall.Stat(testFileA, &nstat)
		test.AssertNoErr(err)
		test.Assert(nstat.Nlink == 3,
			"Nlink incorrect: %d", nstat.Nlink)

		err = syscall.Stat(testFileB, &nstat)
		test.AssertNoErr(err)
		test.Assert(nstat.Nlink == 2,
			"Nlink incorrect: %d", nstat.Nlink)

		// Write another file to ensure the wsr is dirty
		testFileC := workspace + "/testFileC"
		err = testutils.PrintToFile(testFileC, string(data[:1000]))
		test.AssertNoErr(err)

		// trigger a sync so the workspace is published
		test.SyncAllWorkspaces()

		workspaceB := "branch/copyWorkspace/test"
		api := test.getApi()
		err = api.Branch(test.RelPath(workspace), workspaceB)
		test.Assert(err == nil, "Unable to branch")

		wsrB, cleanup := test.GetWorkspaceRoot(workspaceB)
		defer cleanup()

		// ensure that the hardlink was able to sync
		wsrBFileA := test.AbsPath(workspaceB +
			"/subdir/grandchild/linkFileA2")
		readData, err := ioutil.ReadFile(wsrBFileA)
		test.AssertNoErr(err)
		test.Assert(bytes.Equal(readData, data),
			"Data not synced via hardlink")

		stat, err := os.Stat(wsrBFileA)
		test.AssertNoErr(err)
		test.Assert(stat.Size() == int64(len(data)), "file length mismatch")

		test.Assert(len(wsr.hardlinks) == len(wsrB.hardlinks),
			"Hardlink map length not preserved: %v %v", wsr.hardlinks,
			wsrB.hardlinks)

		for k, l := range wsr.hardlinks {
			linkBPtr, exists := wsrB.hardlinks[k]

			test.Assert(l.nlink == linkBPtr.nlink,
				"link reference count not preserved")

			linkB := linkBPtr.record
			v := l.record
			test.Assert(exists, "link not reloaded in new wsr")
			test.Assert(v.Filename() == linkB.Filename(),
				"Filename not preserved")
			test.Assert(v.Type() == linkB.Type(), "Type not preserved")
			test.Assert(v.ID().String() == linkB.ID().String(),
				"ID not preserved")
			test.Assert(v.Size() == linkB.Size(), "Size not preserved")
			test.Assert(v.ModificationTime() == linkB.ModificationTime(),
				"Modtime not preserved")
			test.Assert(v.ContentTime() == linkB.ContentTime(),
				"ContentTime not preserved")
			test.Assert(v.Permissions() == linkB.Permissions(),
				"Permissions not preserved")
			test.Assert(v.Owner() == linkB.Owner(),
				"OwnerID not preserved")
			test.Assert(v.Group() == linkB.Group(),
				"GroupID not preserved")
			test.Assert(v.ExtendedAttributes().String() ==
				linkB.ExtendedAttributes().String(),
				"ExtendedAttributes not preserved")
			test.Assert(v.ContentTime() == linkB.ContentTime(),
				"ContentTime not preserved")
			test.Assert(v.ContentTime() == linkB.ContentTime(),
				"ContentTime not preserved")
		}
	})
}

func TestHardlinkRelay(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		testData := GenData(2000)

		file1 := workspace + "/orig_file"
		err := ioutil.WriteFile(file1, testData[:1000], 0777)
		test.AssertNoErr(err)

		file2 := workspace + "/hardlink"
		err = syscall.Link(file1, file2)
		test.AssertNoErr(err)

		file3 := workspace + "/second_file"
		err = ioutil.WriteFile(file3, testData[:577], 0777)
		test.AssertNoErr(err)

		file4 := workspace + "/hardlink2"
		err = syscall.Link(file3, file4)
		test.AssertNoErr(err)

		// Change file contents
		err = testutils.PrintToFile(file2, string(testData[1000:]))
		test.AssertNoErr(err)

		// Change permissions
		err = os.Chmod(file2, 0654)
		test.AssertNoErr(err)

		// Ensure that file1 changed
		readData, err := ioutil.ReadFile(file1)
		test.Assert(bytes.Equal(readData, testData), "data not linked")

		info, err := os.Stat(file1)
		test.AssertNoErr(err)
		test.Assert(info.Mode().Perm() == 0654, "Permissions not linked")
		test.Assert(info.Size() == int64(len(testData)), "Size not linked")

		infoLink, err := os.Stat(file2)
		test.AssertNoErr(err)
		test.Assert(info.ModTime() == infoLink.ModTime(),
			"hardlink instance modTimes not shared")

		// Ensure that file 3 and file4 didn't
		info2, err := os.Stat(file3)
		test.AssertNoErr(err)
		test.Assert(info.Mode().Perm() != info2.Mode().Perm(),
			"hardlink permissions not separate")
		test.Assert(info.Size() != info2.Size(),
			"hardlink sizes not separate")
		test.Assert(test.getInodeNum(file3) != test.getInodeNum(file1),
			"multiple different hardlinks joined")
		test.Assert(info.ModTime() != info2.ModTime(),
			"hardlink mod times not separate")
	})
}

func TestHardlinkForget(t *testing.T) {
	runTestCustomConfig(t, dirtyDelay100Ms, func(test *testHelper) {
		workspace := test.NewWorkspace()

		data := GenData(2000)

		testFile := workspace + "/testFile"
		test.AssertNoErr(testutils.PrintToFile(testFile, string(data)))

		linkFile := workspace + "/testLink"
		test.AssertNoErr(syscall.Link(testFile, linkFile))

		// Read the hardlink to ensure it's instantiated
		readData, err := ioutil.ReadFile(linkFile)
		test.AssertNoErr(err)
		test.Assert(bytes.Equal(data, readData), "hardlink data mismatch")

		// Forget it
		linkInode := test.getInodeNum(linkFile)
		test.WaitToBeUninstantiated(linkInode)
	})
}

func TestHardlinkUninstantiateDirectory(t *testing.T) {
	// If a hardlink is a child of many directories, it shouldn't prevent those
	// directories from becoming uninstantiated simply because it itself is still
	// instantiated. It is likely being held open by some other directory or
	// handle.
	runTestCustomConfig(t, dirtyDelay100Ms, func(test *testHelper) {
		workspace := test.NewWorkspace()

		data := GenData(2000)
		testCtx := test.newCtx()

		testFile := workspace + "/testFile"
		err := testutils.PrintToFile(testFile, string(data))
		test.AssertNoErr(err)

		dirName := workspace + "/dir"
		err = syscall.Mkdir(dirName, 0777)
		test.AssertNoErr(err)

		linkFile := dirName + "/testLink"
		err = syscall.Link(testFile, linkFile)
		test.AssertNoErr(err)

		// Read the hardlink to ensure it's instantiated
		readData, err := ioutil.ReadFile(linkFile)
		test.AssertNoErr(err)
		test.Assert(bytes.Equal(data, readData), "hardlink data mismatch")

		wsrInode := test.getInodeNum(workspace)
		dirInode := test.getInodeNum(dirName)
		linkInode := test.getInodeNum(linkFile)
		test.qfs.increaseLookupCount(testCtx, linkInode)

		// Check that the directory parent uninstantiated, even if the
		// Hardlink itself cannot be.
		test.WaitToBeUninstantiated(dirInode)

		// Now that dirInode is uninstantiated, retry syncing to make
		// sure this time the linkInode will prevent the uninstantiation
		test.SyncWorkspace(test.RelPath(workspace))

		// Even though the directory "parent" should have been
		// uninstantiated, the WorkspaceRoot must not have been
		// uninstantiated because the hardlink is instantiated.
		msg := fmt.Sprintf("Not all children unloaded, %d in %d", linkInode,
			wsrInode)
		test.WaitFor("WSR to be held by instantiated hardlink",
			func() bool { return test.TestLogContains(msg) })

		test.qfs.shouldForget(testCtx, linkInode, 1)
	})
}

// When all hardlinks, but one, are deleted then we need to convert a hardlink back
// into a regular file.
func TestHardlinkConversion(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		data := GenData(2000)

		testFile := workspace + "/testFile"
		err := testutils.PrintToFile(testFile, string(data[:1000]))
		test.AssertNoErr(err)

		linkFile := workspace + "/testLink"
		err = syscall.Link(testFile, linkFile)
		test.AssertNoErr(err)

		linkInode := test.getInodeNum(linkFile)

		wsr, cleanup := test.GetWorkspaceRoot(workspace)
		defer cleanup()
		fileId := func() quantumfs.FileId {
			defer wsr.linkLock.Lock().Unlock()
			return wsr.inodeToLink[linkInode]
		}()

		err = os.Remove(testFile)
		test.AssertNoErr(err)

		// Ensure it's converted by performing an operation on linkFile
		// that would trigger checking if the hardlink needs conversion
		test.remountFilesystem()

		_, err = os.Stat(linkFile)
		test.AssertNoErr(err)
		test.SyncAllWorkspaces()

		// ensure we can still use the file as normal
		err = testutils.PrintToFile(linkFile, string(data[1000:]))
		test.AssertNoErr(err)

		output, err := ioutil.ReadFile(linkFile)
		test.AssertNoErr(err)
		test.Assert(bytes.Equal(output, data),
			"File not working after conversion from hardlink")

		wsrB, cleanup := test.GetWorkspaceRoot(workspace)
		defer cleanup()
		defer wsrB.linkLock.Lock().Unlock()
		_, exists := wsrB.hardlinks[fileId]
		test.Assert(!exists, "hardlink not converted back to file")
	})
}

func TestHardlinkSubdirChain(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		data := GenData(2000)

		err := syscall.Mkdir(workspace+"/dir", 0777)
		test.AssertNoErr(err)

		testFile := workspace + "/dir/testFile"
		err = testutils.PrintToFile(testFile, string(data))
		test.AssertNoErr(err)

		linkFile := workspace + "/dir/testLink"
		err = syscall.Link(testFile, linkFile)
		test.AssertNoErr(err)

		linkFile2 := workspace + "/dir/testLink2"
		err = syscall.Link(linkFile, linkFile2)
		test.AssertNoErr(err)

		linkFile3 := workspace + "/dir/testLink3"
		err = syscall.Link(linkFile2, linkFile3)
		test.AssertNoErr(err)

		// Now link again from the original
		linkFile4 := workspace + "/dir/testLink4"
		err = syscall.Link(linkFile, linkFile4)
		test.AssertNoErr(err)
	})
}

func TestHardlinkWsrChain(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		data := GenData(2000)

		testFile := workspace + "/testFile"
		err := testutils.PrintToFile(testFile, string(data))
		test.AssertNoErr(err)

		linkFile := workspace + "/testLink"
		err = syscall.Link(testFile, linkFile)
		test.AssertNoErr(err)

		linkFile2 := workspace + "/testLink2"
		err = syscall.Link(linkFile, linkFile2)
		test.AssertNoErr(err)

		linkFile3 := workspace + "/testLink3"
		err = syscall.Link(linkFile2, linkFile3)
		test.AssertNoErr(err)

		// Now link again from the original
		linkFile4 := workspace + "/testLink4"
		err = syscall.Link(linkFile, linkFile4)
		test.AssertNoErr(err)
	})
}

func TestHardlinkInterWorkspace(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspaceA := test.NewWorkspace()
		workspaceB := test.NewWorkspace()

		data := GenData(1000)

		testFile := workspaceA + "/testFile"
		err := testutils.PrintToFile(testFile, string(data))
		test.AssertNoErr(err)

		linkFileA := workspaceA + "/testLink"
		err = syscall.Link(testFile, linkFileA)
		test.AssertNoErr(err)

		linkFail := workspaceB + "/testLinkFail"
		err = syscall.Link(linkFileA, linkFail)
		test.Assert(err != nil,
			"qfs allows existing link copy to another wsr")
		test.Assert(os.IsPermission(err),
			"qfs not returning EPERM for inter-wsr link")

		testFileB := workspaceA + "/testFileB"
		err = testutils.PrintToFile(testFileB, string(data))
		test.AssertNoErr(err)

		linkFailB := workspaceB + "/testLinkFailB"
		err = syscall.Link(testFileB, linkFailB)
		test.Assert(err != nil,
			"qfs allows creation of hardlink across workspace bounds")
		test.Assert(os.IsPermission(err),
			"qfs not returning EPERM for link across wsrs")
	})
}

func TestHardlinkOpenUnlink(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		filename := workspace + "/file"
		linkname := workspace + "/link"

		file, err := os.Create(filename)
		test.AssertNoErr(err)
		defer file.Close()

		file.WriteString("stuff")

		err = os.Link(filename, linkname)
		test.AssertNoErr(err)

		err = os.Remove(filename)
		test.AssertNoErr(err)

		err = os.Remove(linkname)
		test.AssertNoErr(err)
	})
}

func matchXAttrHardlinkExtendedKey(path string, extendedKey []byte,
	test *testHelper, Type quantumfs.ObjectType, wsr *WorkspaceRoot) {

	key, type_, size, err := quantumfs.DecodeExtendedKey(string(extendedKey))
	test.Assert(err == nil, "Error decompressing the packet")

	// Extract the internal ObjectKey from QuantumFS
	inode := test.getInode(path)
	// parent should be the workspace root.
	isHardlink, fileId := wsr.checkHardlink(inode.inodeNum())
	test.Assert(isHardlink, "Expected hardlink isn't one.")

	valid, record := wsr.getHardlink(fileId)
	test.Assert(valid, "Unable to get hardlink from wsr")

	// Verify the type and key matching
	test.Assert(type_ == Type && size == record.Size() &&
		bytes.Equal(key.Value(), record.ID().Value()),
		"Error getting the key: %v with size of %d-%d, keys of %v-%v",
		err, Type, type_, key.Value(), record.ID().Value())
}

func TestHardlinkExtraction(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		filename := workspace + "/file"
		linkname := workspace + "/link"

		file, err := os.Create(filename)
		test.AssertNoErr(err)
		file.WriteString("stuff")
		file.Close()

		err = os.Link(filename, linkname)
		test.AssertNoErr(err)

		dst := make([]byte, quantumfs.ExtendedKeyLength)
		sz, err := syscall.Getxattr(filename, quantumfs.XAttrTypeKey, dst)
		test.Assert(err == nil && sz == quantumfs.ExtendedKeyLength,
			"Error getting the file key: %v with a size of %d",
			err, sz)

		wsr, cleanup := test.GetWorkspaceRoot(workspace)
		defer cleanup()
		matchXAttrHardlinkExtendedKey(filename, dst, test,
			quantumfs.ObjectTypeSmallFile, wsr)

		dst = make([]byte, quantumfs.ExtendedKeyLength)
		sz, err = syscall.Getxattr(filename, quantumfs.XAttrTypeKey, dst)
		test.Assert(err == nil && sz == quantumfs.ExtendedKeyLength,
			"Error getting the file key: %v with a size of %d",
			err, sz)

		matchXAttrHardlinkExtendedKey(linkname, dst, test,
			quantumfs.ObjectTypeSmallFile, wsr)
	})
}

func TestHardlinkRename(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		filename := workspace + "/file"
		linkname := workspace + "/link"

		files := make([]string, 0)

		data := GenData(2000)
		file, err := os.Create(filename)
		test.AssertNoErr(err)
		file.WriteString(string(data))
		file.Close()

		err = os.Link(filename, linkname)
		test.AssertNoErr(err)

		newLink := workspace + "/linkB"
		err = os.Rename(linkname, newLink)
		test.AssertNoErr(err)
		linkname = newLink

		err = syscall.Mkdir(workspace+"/dir", 0777)
		test.AssertNoErr(err)

		newLink = workspace + "/dir/linkC"
		err = os.Rename(linkname, newLink)
		test.AssertNoErr(err)
		linkname = newLink
		files = append(files, linkname)

		err = os.Link(filename, workspace+"/dir/linkE")
		test.AssertNoErr(err)
		files = append(files, workspace+"/dir/linkE")

		err = os.Rename(filename, workspace+"/linkD")
		test.AssertNoErr(err)
		files = append(files, workspace+"/linkD")

		for _, v := range files {
			readback, err := ioutil.ReadFile(v)
			test.AssertNoErr(err)
			test.Assert(bytes.Equal(readback, data),
				"file %s data not preserved", v)
		}
	})
}

func TestHardlinkReparentRace(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		var stat syscall.Stat_t
		iterations := 50
		for i := 0; i < iterations; i++ {
			filename := fmt.Sprintf(workspace+"/file%d", i)
			linkname := fmt.Sprintf(workspace+"/link%d", i)
			file, err := os.Create(filename)
			test.AssertNoErr(err)

			err = syscall.Link(filename, linkname)
			test.AssertNoErr(err)

			file.WriteString("this is file data")
			file.Close()

			parent := test.getInode(workspace)

			// We want to race the parent change with getting the parent
			go os.Remove(filename)
			go ManualLookup(&test.qfs.c, parent, filename)
			go syscall.Stat(filename, &stat)
			go os.Remove(linkname)
		}
	})
}

func TestHardlinkUninstantiated(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		err := utils.MkdirAll(workspace+"/subdir/grandchild", 0777)
		test.AssertNoErr(err)

		filename := workspace + "/subdir/fileA"
		linkname := workspace + "/subdir/grandchild/fileB"
		data := GenData(2000)

		err = testutils.PrintToFile(filename, string(data))
		test.AssertNoErr(err)

		err = syscall.Link(filename, linkname)
		test.AssertNoErr(err)

		// trigger a sync so the workspace is published
		test.SyncAllWorkspaces()

		workspaceB := "branch/copyWorkspace/test"
		api := test.getApi()
		err = api.Branch(test.RelPath(workspace), workspaceB)
		test.AssertNoErr(err)

		readData, err := ioutil.ReadFile(test.AbsPath(workspaceB +
			"/subdir/grandchild/fileB"))
		test.AssertNoErr(err)
		test.Assert(bytes.Equal(readData, data),
			"data mismatch after Branch")
	})
}

func (test *testHelper) LinkFileExp(path string, filename string) {
	err := utils.MkdirAll(path, 0777)
	test.AssertNoErr(err)

	// Enough data to consume a multi block file
	data := GenData(quantumfs.MaxBlockSize + 1000)

	filepath := path + "/" + filename
	linkpath := path + "/" + filename + "link"
	err = testutils.PrintToFile(filepath, string(data[:1000]))
	test.AssertNoErr(err)

	// Make them a link
	err = syscall.Link(filepath, linkpath)
	test.AssertNoErr(err)

	// Cause the underlying file to expand and change its own type
	err = testutils.PrintToFile(linkpath, string(data[1000:]))
	test.AssertNoErr(err)

	// Ensure that the file actually works
	readData, err := ioutil.ReadFile(linkpath)
	test.AssertNoErr(err)
	test.Assert(bytes.Equal(readData, data), "Link data wrong after expansion")
}

func TestHardlinkFileExpansionInWsr(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		test.LinkFileExp(workspace, "fileA")
	})
}

func TestHardlinkFileExpansionOutWsr(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		test.LinkFileExp(workspace+"/dirB", "fileB")
	})
}

// Once a hardlink record is returned to a class for use, the hardlink may be
// unlinked before the record is used. We need to accommodate that.
func TestHardlinkRecordRace(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()
		data := GenData(100)

		// This is a race condition, so repeat to increase the likelihood
		for i := 0; i < 100; i++ {
			filename := fmt.Sprintf("%s/file%d", workspace, i)
			err := testutils.PrintToFile(filename, string(data))
			test.AssertNoErr(err)

			err = syscall.Link(filename, filename+"link")
			test.AssertNoErr(err)

			for i := 0; i < 10; i++ {
				go os.Stat(filename)
			}
			// quickly remove the link before all of the GetAttrs finish
			errA := os.Remove(filename)
			errB := os.Remove(filename + "link")
			test.AssertNoErr(errA)
			test.AssertNoErr(errB)
		}
	})
}

func TestHardlinkDeleteFromDirectory(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		dir1 := workspace + "/dir1/dir1.1"
		err := utils.MkdirAll(dir1, 0777)
		test.AssertNoErr(err)

		dir2 := workspace + "/dir2"
		err = utils.MkdirAll(dir2, 0777)
		test.AssertNoErr(err)

		filename := dir1 + "/fileA"
		linkname := dir2 + "/link"
		data := GenData(2000)

		err = testutils.PrintToFile(filename, string(data))
		test.AssertNoErr(err)

		err = syscall.Link(filename, linkname)
		test.AssertNoErr(err)

		err = os.RemoveAll(dir1)
		test.AssertNoErr(err)
	})
}

func (th *TestHelper) getHardlinkLeg(parentPath string,
	leg string) *HardlinkLeg {

	parent := th.getInode(parentPath)
	parentDir := asDirectory(parent)

	defer parentDir.childRecordLock.Lock().Unlock()
	record := parentDir.children.recordByName(&th.qfs.c, leg).Clone()
	return record.(*HardlinkLeg)
}

func TestHardlinkCreatedTime(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		test.AssertNoErr(utils.MkdirAll(workspace+"/dirA", 0777))

		dirA := workspace + "/dirA"
		fileA := dirA + "/fileA"
		fileB := dirA + "/fileB"
		fileC := workspace + "/fileC"
		fileD := workspace + "/fileD"
		fileE := dirA + "/fileE"

		test.AssertNoErr(testutils.PrintToFile(fileA, "dataA"))
		test.AssertNoErr(syscall.Link(fileA, fileB))

		test.AssertNoErr(testutils.PrintToFile(fileC, "dataC"))
		test.AssertNoErr(syscall.Link(fileC, fileD))
		test.AssertNoErr(syscall.Link(fileD, fileE))

		recordA := test.getHardlinkLeg(dirA, "fileA")
		recordB := test.getHardlinkLeg(dirA, "fileB")
		recordC := test.getHardlinkLeg(workspace, "fileC")
		recordD := test.getHardlinkLeg(workspace, "fileD")
		recordE := test.getHardlinkLeg(dirA, "fileE")

		var statA, statB, statC, statD, statE syscall.Stat_t
		test.AssertNoErr(syscall.Stat(fileA, &statA))
		test.AssertNoErr(syscall.Stat(fileB, &statB))
		test.AssertNoErr(syscall.Stat(fileC, &statC))
		test.AssertNoErr(syscall.Stat(fileD, &statD))
		test.AssertNoErr(syscall.Stat(fileE, &statE))

		test.Assert(statA.Ctim == statB.Ctim, "First link time changed")
		test.Assert(statC.Ctim == statD.Ctim && statD.Ctim == statE.Ctim,
			"Second link time changed")

		test.Assert(recordA.creationTime < recordB.creationTime &&
			recordB.creationTime != recordC.creationTime &&
			recordC.creationTime < recordD.creationTime &&
			recordD.creationTime < recordE.creationTime,
			"Records not all different: %d %d %d %d %d",
			recordA.creationTime, recordB.creationTime,
			recordC.creationTime, recordD.creationTime,
			recordE.creationTime)

		test.Assert(recordA.creationTime != quantumfs.Time(0) &&
			recordB.creationTime != quantumfs.Time(0) &&
			recordC.creationTime != quantumfs.Time(0) &&
			recordD.creationTime != quantumfs.Time(0) &&
			recordE.creationTime != quantumfs.Time(0),
			"hardlink instance creationTime time not set")

		// ensure creationTime field is preserved across branching
		workspaceB := "branch/copyWorkspace/test"
		api := test.getApi()
		test.AssertNoErr(api.Branch(test.RelPath(workspace), workspaceB))
		workspaceB = test.AbsPath(workspaceB)

		dirA = workspaceB + "/dirA"
		// Read a file from the branched workspace to ensure they instantiate
		_, err := ioutil.ReadFile(dirA + "/fileA")
		test.AssertNoErr(err)

		recordA2 := test.getHardlinkLeg(dirA, "fileA")
		recordB2 := test.getHardlinkLeg(dirA, "fileB")
		recordC2 := test.getHardlinkLeg(workspaceB, "fileC")
		recordD2 := test.getHardlinkLeg(workspaceB, "fileD")
		recordE2 := test.getHardlinkLeg(dirA, "fileE")

		test.Assert(recordA.creationTime == recordA2.creationTime &&
			recordB.creationTime == recordB2.creationTime &&
			recordC.creationTime == recordC2.creationTime &&
			recordD.creationTime == recordD2.creationTime &&
			recordE.creationTime == recordE2.creationTime,
			"creationTime field not preserved across branching, "+
				"%d %d, %d %d, %d %d, %d %d, %d %d",
			recordA.creationTime, recordA2.creationTime,
			recordB.creationTime, recordB2.creationTime,
			recordC.creationTime, recordC2.creationTime,
			recordD.creationTime, recordD2.creationTime,
			recordE.creationTime, recordE2.creationTime)
	})
}

// test to ensure that renaming a hardlink resets its creationTime
func TestHardlinkRenameCreation(t *testing.T) {
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		dirA := workspace + "/dirA"
		dirB := workspace + "/dirA/dirB"
		test.AssertNoErr(utils.MkdirAll(dirB, 0777))

		fileA := dirA + "/fileA"
		fileB := dirA + "/fileB"
		fileC := dirA + "/fileC"
		fileD := dirB + "/fileD"

		test.AssertNoErr(testutils.PrintToFile(fileA, "dataA"))
		test.AssertNoErr(syscall.Link(fileA, fileB))

		recordA := test.getHardlinkLeg(dirA, "fileA")
		recordB := test.getHardlinkLeg(dirA, "fileB")

		test.AssertNoErr(os.Rename(fileA, fileC))
		recordC := test.getHardlinkLeg(dirA, "fileC")

		test.AssertNoErr(os.Rename(fileB, fileD))
		recordD := test.getHardlinkLeg(dirB, "fileD")

		// test both rename and mvchild
		test.Assert(recordA.creationTime < recordC.creationTime,
			"Rename of hardlink doesn't reset creationTime")
		test.Assert(recordB.creationTime < recordD.creationTime,
			"Mvchild of hardlink doesn't reset creationTime")
	})
}

func TestRemoveHardlinkBeforeSync(t *testing.T) {
	t.Skip() // BUG224729
	runTest(t, func(test *testHelper) {
		workspace := test.NewWorkspace()

		dirName := workspace + "/dir"
		test.AssertNoErr(utils.MkdirAll(dirName, 0777))

		wsr, cleanup := test.GetWorkspaceRoot(workspace)
		defer cleanup()

		test.Assert(len(wsr.hardlinks) == 0,
			"Hardlink table not initially empty")

		// Create and remove the hardlink. Though the file is gone the
		// hardink entry must remain until after all the directories in which
		// it was a child have flushed, otherwise an uploaded directory
		// metadata may point to an entry which does not exist in the
		// hardlink table.
		test.createFile(workspace, "dir/leg1", 1)
		test.linkFile(workspace, "dir/leg1", "leg2")
		test.removeFile(workspace, "leg2")
		test.removeFile(workspace, "dir/leg1")

		test.Assert(len(wsr.hardlinks) == 1,
			"Hardlink table doesn't contain entry after delete")

		test.SyncWorkspace(test.RelPath(workspace))

		test.Assert(len(wsr.hardlinks) == 0,
			"Hardlink table not empty after sync")
	})
}

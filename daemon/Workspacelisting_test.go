// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test some special properties of workspacelisting type

import "testing"
import "github.com/aristanetworks/quantumfs"

func verifyWorkspacelistingInodeStatus(c *ctx, test *testHelper,
	name string, space string, mustBeInstantiated bool,
	inodeMap *map[string]InodeId) InodeId {

	id, exists := (*inodeMap)[name]
	test.assert(exists, "Fail to get the inodeId of %s", space)

	inode := test.qfs.inodeNoInstantiate(c, id)
	if mustBeInstantiated {
		test.assert(inode != nil,
			"The %s should be instantiated", space)
	} else {
		test.assert(inode == nil,
			"The %s should be uninstantiated", space)
	}

	return id
}

func TestWorkspacelistingInstantiateOnDemand(t *testing.T) {
	runTest(t, func(test *testHelper) {

		c := test.newCtx()
		tslInode := test.qfs.inodeNoInstantiate(c, quantumfs.InodeIdRoot)
		tsl := tslInode.(*TypespaceList)

		workspace := test.newWorkspaceWithoutWritePerm()
		type_, name_, work_ := test.getWorkspaceComponents(workspace)
		_, exists := tsl.typespacesByName[type_]
		test.assert(!exists,
			"Error getting a non-existing inodeId of typespace")

		// Enabling the write permission in the new workspace can trigger
		// updateChildren in workspacelisting. Map within will be updated,
		// so inodes in the proceeding workspace will be valid right now.
		test.newWorkspace()

		// Verify that the typespace has been assigned an ID to but not
		// instantiated yet. If the inode is not created, there is no
		// need to verify its descendents: namespace and workspace.
		verifyWorkspacelistingInodeStatus(c, test, type_, "typespace",
			false, &tsl.typespacesByName)

		// Enabling write permission can also update workspacelisting
		// Instantiate the three inodes and verify the existence
		api := test.getApi()
		err := api.EnableRootWrite(test.relPath(workspace))
		test.assert(err == nil, "Failed to enable write permission in "+
			"workspace: %v", err)

		nslId := verifyWorkspacelistingInodeStatus(c, test, type_,
			"typespace", true, &tsl.typespacesByName)
		nslInode := test.qfs.inodeNoInstantiate(c, nslId)
		nsl := nslInode.(*NamespaceList)

		wslId := verifyWorkspacelistingInodeStatus(c, test, name_,
			"namespace", true, &nsl.namespacesByName)
		wslInode := test.qfs.inodeNoInstantiate(c, wslId)
		wsl := wslInode.(*WorkspaceList)

		verifyWorkspacelistingInodeStatus(c, test, work_, "workspace",
			true, &wsl.workspacesByName)
	})
}

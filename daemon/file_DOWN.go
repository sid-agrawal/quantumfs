// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// This is the _DOWN counterpart to file.go

import "github.com/aristanetworks/quantumfs"
import "github.com/hanwen/go-fuse/fuse"

func (fi *File) link_DOWN(c *ctx, srcInode Inode, newName string,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Link on File")
	return fuse.ENOTDIR
}

func (fi *File) flush_DOWN(c *ctx) quantumfs.ObjectKey {
	defer c.FuncIn("File::flush_DOWN", "%s", fi.name_).out()

	key := fi.accessor.sync(c)
	fi.parent(c).syncChild(c, fi.inodeNum(), key)
	return key
}

func (fi *File) Sync_DOWN(c *ctx) fuse.Status {
	return fuse.OK
}

func (fd *FileDescriptor) Sync_DOWN(c *ctx) fuse.Status {
	defer c.funcIn("File::Sync_DOWN").out()

	defer fd.file.Lock().Unlock()
	if fd.file.isDirty() {
		fd.file.flush_DOWN(c)
	}

	return fuse.OK
}

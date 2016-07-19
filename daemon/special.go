// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// This file holds the special type, which represents devices files, fifos and unix
// domain sockets

import "encoding/binary"
import "errors"

import "github.com/aristanetworks/quantumfs"

import "github.com/hanwen/go-fuse/fuse"

func newSpecial(c *ctx, key quantumfs.ObjectKey, size uint64, inodeNum InodeId,
	parent Inode, mode uint32, rdev uint32,
	dirRecord *quantumfs.DirectoryRecord) Inode {

	if key.Type() != quantumfs.KeyTypeEmbedded {
		panic("Non-embedded key when initializing Special file")
	}

	hash := key.Hash()

	special := Special{
		InodeCommon: InodeCommon{
			id:        inodeNum,
			treeLock_: parent.treeLock(),
		},
		filetype: binary.LittleEndian.Uint32(hash[0:4]),
		device:   binary.LittleEndian.Uint32(hash[4:8]),
		parent:   parent,
	}
	special.self = &special
	assert(special.treeLock() != nil, "Special treeLock nil at init")
	return &special
}

type Special struct {
	InodeCommon
	filetype uint32
	device   uint32
	parent   Inode
}

func (special *Special) Access(c *ctx, mask uint32, uid uint32,
	gid uint32) fuse.Status {

	return fuse.OK
}

func (special *Special) GetAttr(c *ctx, out *fuse.AttrOut) fuse.Status {
	record, err := special.parent.getChildRecord(c, special.InodeCommon.id)
	if err != nil {
		c.elog("Unable to get record from parent for inode %d", special.id)
		return fuse.EIO
	}

	fillAttrOutCacheData(c, out)
	fillAttrWithDirectoryRecord(c, &out.Attr, special.InodeCommon.id,
		c.fuseCtx.Owner, &record)

	return fuse.OK
}

func (special *Special) Lookup(c *ctx, name string, out *fuse.EntryOut) fuse.Status {
	c.elog("Invalid Lookup call on Special")
	return fuse.ENOSYS
}

func (special *Special) Open(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	c.elog("Invalid Open call on Special")
	return fuse.ENOSYS
}

func (special *Special) OpenDir(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	return fuse.ENOTDIR
}

func (special *Special) Create(c *ctx, input *fuse.CreateIn, name string,
	out *fuse.CreateOut) fuse.Status {

	return fuse.ENOTDIR
}

func (special *Special) SetAttr(c *ctx, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	return special.parent.setChildAttr(c, special.InodeCommon.id, nil, attr, out)
}

func (special *Special) Mkdir(c *ctx, name string, input *fuse.MkdirIn,
	out *fuse.EntryOut) fuse.Status {

	return fuse.ENOTDIR
}

func (special *Special) Unlink(c *ctx, name string) fuse.Status {
	c.elog("Invalid Unlink on Special")
	return fuse.ENOTDIR
}

func (special *Special) Rmdir(c *ctx, name string) fuse.Status {
	c.elog("Invalid Rmdir on Special")
	return fuse.ENOTDIR
}

func (special *Special) Symlink(c *ctx, pointedTo string, specialName string,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Symlink on Special")
	return fuse.ENOTDIR
}

func (special *Special) Readlink(c *ctx) ([]byte, fuse.Status) {
	c.elog("Invalid Readlink on Special")
	return nil, fuse.EINVAL
}

func (special *Special) Sync(c *ctx) fuse.Status {
	key := special.sync_DOWN(c)
	special.parent.syncChild(c, special.InodeCommon.id, key)

	return fuse.OK
}

func (special *Special) Mknod(c *ctx, name string, input *fuse.MknodIn,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Mknod on Special")
	return fuse.ENOSYS
}

func (special *Special) syncChild(c *ctx, inodeNum InodeId,
	newKey quantumfs.ObjectKey) {

	c.elog("Invalid syncChild on Special")
}

func (special *Special) setChildAttr(c *ctx, inodeNum InodeId,
	newType *quantumfs.ObjectType, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	c.elog("Invalid setChildAttr on Special")
	return fuse.ENOSYS
}

func (special *Special) getChildRecord(c *ctx,
	inodeNum InodeId) (quantumfs.DirectoryRecord, error) {

	c.elog("Unsupported record fetch on Special")
	return quantumfs.DirectoryRecord{}, errors.New("Unsupported record fetch")
}

func (special *Special) dirty(c *ctx) {
	special.setDirty(true)
	special.parent.dirtyChild(c, special)
}

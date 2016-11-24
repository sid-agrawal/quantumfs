// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// The code which handles listing available workspaces as the first two levels of the
// directory hierarchy.
package daemon

import "errors"
import "sync"
import "syscall"
import "time"

import "github.com/aristanetworks/quantumfs"
import "github.com/hanwen/go-fuse/fuse"

func NewNamespaceList() Inode {
	nsl := NamespaceList{
		InodeCommon:      InodeCommon{id: quantumfs.InodeIdRoot},
		namespacesByName: make(map[string]InodeId),
		namespacesById:   make(map[InodeId]string),
	}
	nsl.self = &nsl
	nsl.InodeCommon.treeLock_ = &nsl.realTreeLock
	assert(nsl.treeLock() != nil, "NamespaceList treeLock nil at init")
	return &nsl
}

type NamespaceList struct {
	InodeCommon

	// Map from child name to Inode ID
	namespacesByName map[string]InodeId
	namespacesById   map[InodeId]string

	realTreeLock sync.RWMutex
}

func (nsl *NamespaceList) dirty(c *ctx) {
}

func (nsl *NamespaceList) dirtyChild(c *ctx, child Inode) {
}

func (nsl *NamespaceList) Access(c *ctx, mask uint32, uid uint32,
	gid uint32) fuse.Status {

	c.elog("Unsupported Access on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) GetAttr(c *ctx, out *fuse.AttrOut) fuse.Status {
	out.AttrValid = c.config.CacheTimeSeconds
	out.AttrValidNsec = c.config.CacheTimeNsecs

	fillRootAttr(c, &out.Attr, nsl.InodeCommon.id)
	return fuse.OK
}

func (nsl *NamespaceList) markSelfAccessed(c *ctx, created bool) {
	nsl.markAccessed(c, "", created)
	return
}

func (nsl *NamespaceList) markAccessed(c *ctx, path string, created bool) {
	c.elog("Invalid markAccessed on NamespaceList")
	return
}

func fillRootAttr(c *ctx, attr *fuse.Attr, inodeNum InodeId) {
	num, err := c.workspaceDB.NumNamespaces(&c.Ctx)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	fillAttr(attr, inodeNum, uint32(num))
}

type listingAttrFill func(c *ctx, attr *fuse.Attr, inodeNum InodeId, name string)

func fillNamespaceAttr(c *ctx, attr *fuse.Attr, inodeNum InodeId, namespace string) {
	num, err := c.workspaceDB.NumWorkspaces(&c.Ctx, namespace)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	fillAttr(attr, inodeNum, uint32(num))
}

func fillAttr(attr *fuse.Attr, inodeNum InodeId, numChildren uint32) {
	attr.Ino = uint64(inodeNum)
	attr.Size = 4096
	attr.Blocks = 1

	now := time.Now()
	attr.Atime = uint64(now.Unix())
	attr.Atimensec = uint32(now.Nanosecond())
	attr.Mtime = uint64(now.Unix())
	attr.Mtimensec = uint32(now.Nanosecond())

	attr.Ctime = 1
	attr.Ctimensec = 1
	attr.Mode = 0555 | fuse.S_IFDIR
	attr.Nlink = 2 + numChildren
	attr.Owner.Uid = 0
	attr.Owner.Gid = 0
	attr.Blksize = 4096
}

func fillEntryOutCacheData(c *ctx, out *fuse.EntryOut) {
	out.Generation = 1
	out.EntryValid = c.config.CacheTimeSeconds
	out.EntryValidNsec = c.config.CacheTimeNsecs
	out.AttrValid = c.config.CacheTimeSeconds
	out.AttrValidNsec = c.config.CacheTimeNsecs
}

func fillAttrOutCacheData(c *ctx, out *fuse.AttrOut) {
	out.AttrValid = c.config.CacheTimeSeconds
	out.AttrValidNsec = c.config.CacheTimeNsecs
}

// Update the internal namespaces list with the most recent available listing
func updateChildren(c *ctx, parentName string, names []string,
	inodeMap *map[string]InodeId, nameMap *map[InodeId]string, parent Inode,
	newInode func(c *ctx, parentName string, name string, parent Inode,
		inodeId InodeId) Inode) {

	touched := make(map[string]bool)

	// First add any new entries
	for _, name := range names {
		if _, exists := (*inodeMap)[name]; !exists {
			inodeId := c.qfs.newInodeId()
			(*inodeMap)[name] = inodeId
			(*nameMap)[inodeId] = name
			if parentName == "_null" && name == "null" {
				c.qfs.setInode(c, inodeId, newNullWorkspaceRoot(c,
					parentName, name, parent, inodeId))
			} else {
				c.qfs.setInode(c, inodeId, newInode(c, parentName,
					name, parent, inodeId))
			}
		}
		touched[name] = true
	}

	// Then delete entries which no longer exist
	for name, _ := range *inodeMap {
		if _, exists := touched[name]; !exists {
			inodeNum := (*inodeMap)[name]
			c.qfs.setInode(c, inodeNum, nil)
			delete(*inodeMap, name)
			delete(*nameMap, inodeNum)
		}
	}
}

func snapshotChildren(c *ctx, children *map[string]InodeId,
	fillAttr listingAttrFill) []directoryContents {

	out := make([]directoryContents, 0, len(*children))
	for name, inode := range *children {
		child := directoryContents{
			filename: name,
			fuseType: fuse.S_IFDIR,
		}
		fillAttr(c, &child.attr, inode, name)

		out = append(out, child)
	}

	return out
}

func (nsl *NamespaceList) Open(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	return fuse.ENOSYS
}

func (nsl *NamespaceList) OpenDir(c *ctx, flags uint32,
	mode uint32, out *fuse.OpenOut) fuse.Status {

	ds := newDirectorySnapshot(c, nsl)
	c.qfs.setFileHandle(c, ds.FileHandleCommon.id, ds)
	out.Fh = uint64(ds.FileHandleCommon.id)
	out.OpenFlags = 0

	return fuse.OK
}

func (nsl *NamespaceList) getChildSnapshot(c *ctx) []directoryContents {
	list, err := c.workspaceDB.NamespaceList(&c.Ctx)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	updateChildren(c, "/", list, &nsl.namespacesByName,
		&nsl.namespacesById, nsl, newWorkspaceList)
	children := snapshotChildren(c, &nsl.namespacesByName, fillNamespaceAttr)

	api := directoryContents{
		filename: quantumfs.ApiPath,
		fuseType: fuse.S_IFREG,
	}
	fillApiAttr(&api.attr)
	children = append(children, api)

	return children
}

func (nsl *NamespaceList) Lookup(c *ctx, name string,
	out *fuse.EntryOut) fuse.Status {

	if name == quantumfs.ApiPath {
		out.NodeId = quantumfs.InodeIdApi
		fillEntryOutCacheData(c, out)
		fillApiAttr(&out.Attr)
		return fuse.OK
	}

	exists, err := c.workspaceDB.NamespaceExists(&c.Ctx, name)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	if !exists {
		return fuse.ENOENT
	}

	var list []string
	list, err = c.workspaceDB.NamespaceList(&c.Ctx)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	updateChildren(c, "/", list,
		&nsl.namespacesByName, &nsl.namespacesById, nsl, newWorkspaceList)

	inodeNum := nsl.namespacesByName[name]
	out.NodeId = uint64(inodeNum)
	fillEntryOutCacheData(c, out)
	fillNamespaceAttr(c, &out.Attr, inodeNum, name)

	return fuse.OK
}

func (nsl *NamespaceList) Create(c *ctx, input *fuse.CreateIn, name string,
	out *fuse.CreateOut) fuse.Status {

	return fuse.EACCES
}

func (nsl *NamespaceList) SetAttr(c *ctx, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	c.elog("Invalid SetAttr on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) Mkdir(c *ctx, name string, input *fuse.MkdirIn,
	out *fuse.EntryOut) fuse.Status {

	return fuse.EPERM
}

func (nsl *NamespaceList) getChildRecord(c *ctx,
	inodeNum InodeId) (quantumfs.DirectoryRecord, error) {

	c.elog("Unsupported record fetch on NamespaceList")
	return quantumfs.DirectoryRecord{},
		errors.New("Unsupported record fetch on NamespaceList")
}

func (nsl *NamespaceList) Unlink(c *ctx, name string) fuse.Status {
	c.elog("Invalid Unlink on NamespaceList")
	return fuse.ENOTDIR
}

func (nsl *NamespaceList) Rmdir(c *ctx, name string) fuse.Status {
	c.elog("Invalid Rmdir on NamespaceList")
	return fuse.EACCES
}

func (nsl *NamespaceList) Symlink(c *ctx, pointedTo string, linkName string,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Symlink on NamespaceList")
	return fuse.EACCES
}

func (nsl *NamespaceList) Readlink(c *ctx) ([]byte, fuse.Status) {
	c.elog("Invalid Readlink on NamespaceList")
	return nil, fuse.EINVAL
}

func (nsl *NamespaceList) Mknod(c *ctx, name string, input *fuse.MknodIn,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Mknod on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) RenameChild(c *ctx, oldName string,
	newName string) fuse.Status {

	c.elog("Invalid RenameChild on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) MvChild(c *ctx, dstInode Inode, oldName string,
	newName string) fuse.Status {

	c.elog("Invalid MvChild on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) GetXAttrSize(c *ctx,
	attr string) (size int, result fuse.Status) {

	c.elog("Invalid GetXAttrSize on NamespaceList")
	return 0, fuse.ENODATA
}

func (nsl *NamespaceList) GetXAttrData(c *ctx,
	attr string) (data []byte, result fuse.Status) {

	c.elog("Invalid GetXAttrData on NamespaceList")
	return nil, fuse.ENODATA
}

func (nsl *NamespaceList) ListXAttr(c *ctx) (attributes []byte, result fuse.Status) {
	c.elog("Invalid ListXAttr on NamespaceList")
	return []byte{}, fuse.OK
}

func (nsl *NamespaceList) SetXAttr(c *ctx, attr string, data []byte) fuse.Status {
	c.elog("Invalid SetXAttr on NamespaceList")
	return fuse.Status(syscall.ENOSPC)
}

func (nsl *NamespaceList) RemoveXAttr(c *ctx, attr string) fuse.Status {
	c.elog("Invalid RemoveXAttr on NamespaceList")
	return fuse.ENODATA
}

func (nsl *NamespaceList) syncChild(c *ctx, inodeNum InodeId,
	newKey quantumfs.ObjectKey) {

	c.elog("Invalid syncChild on NamespaceList for %d", inodeNum)
}

func (nsl *NamespaceList) setChildAttr(c *ctx, inodeNum InodeId,
	newType *quantumfs.ObjectType, attr *fuse.SetAttrIn,
	out *fuse.AttrOut, updateMtime bool) fuse.Status {

	c.elog("Invalid setChildAttr on NamespaceList")
	return fuse.ENOSYS
}

func (nsl *NamespaceList) getChildXAttrSize(c *ctx, inodeNum InodeId,
	attr string) (size int, result fuse.Status) {

	c.elog("Invalid getChildXAttrSize on NamespaceList")
	return 0, fuse.ENODATA
}

func (nsl *NamespaceList) getChildXAttrData(c *ctx, inodeNum InodeId,
	attr string) (data []byte, result fuse.Status) {

	c.elog("Invalid getChildXAttrData on NamespaceList")
	return nil, fuse.ENODATA
}

func (nsl *NamespaceList) listChildXAttr(c *ctx,
	inodeNum InodeId) (attributes []byte, result fuse.Status) {

	c.elog("Invalid listChildXAttr on NamespaceList")
	return []byte{}, fuse.OK
}

func (nsl *NamespaceList) setChildXAttr(c *ctx, inodeNum InodeId, attr string,
	data []byte) fuse.Status {

	c.elog("Invalid setChildXAttr on NamespaceList")
	return fuse.Status(syscall.ENOSPC)
}

func (nsl *NamespaceList) removeChildXAttr(c *ctx, inodeNum InodeId,
	attr string) fuse.Status {

	c.elog("Invalid removeChildXAttr on NamespaceList")
	return fuse.ENODATA
}

func (nsl *NamespaceList) instantiateChild(c *ctx,
	inodeNum InodeId) (Inode, []InodeId) {

	c.vlog("NamespaceList::instantiateChild Enter")
	defer c.vlog("NamespaceList::instantiateChild Exit")

	// The api file will never be truly forgotten (see QuantumFs.Forget()) and so
	// doesn't need to ever be re-instantiated.

	name, exists := nsl.namespacesById[inodeNum]
	if exists {
		c.vlog("Instantiating %d -> %s", inodeNum, name)
	} else {
		c.vlog("inode %d doesn't exist", inodeNum)
	}

	return newWorkspaceList(c, "/", nsl.namespacesById[inodeNum], nsl, inodeNum),
		nil
}

func newWorkspaceList(c *ctx, parentName string, name string, parent Inode,
	inodeNum InodeId) Inode {

	wsl := WorkspaceList{
		InodeCommon:      InodeCommon{id: inodeNum},
		namespaceName:    name,
		workspacesByName: make(map[string]InodeId),
		workspacesById:   make(map[InodeId]string),
	}
	wsl.self = &wsl
	wsl.setParent(parent)
	wsl.InodeCommon.treeLock_ = &wsl.realTreeLock
	assert(wsl.treeLock() != nil, "WorkspaceList treeLock nil at init")
	return &wsl
}

type WorkspaceList struct {
	InodeCommon
	namespaceName string

	// Map from child name to Inode ID
	workspacesByName map[string]InodeId
	workspacesById   map[InodeId]string

	realTreeLock sync.RWMutex
}

func (wsl *WorkspaceList) dirty(c *ctx) {
}

func (wsl *WorkspaceList) dirtyChild(c *ctx, child Inode) {
}

func (wsl *WorkspaceList) Access(c *ctx, mask uint32, uid uint32,
	gid uint32) fuse.Status {

	c.elog("Unsupported Access on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) GetAttr(c *ctx, out *fuse.AttrOut) fuse.Status {
	out.AttrValid = c.config.CacheTimeSeconds
	out.AttrValidNsec = c.config.CacheTimeNsecs

	fillRootAttr(c, &out.Attr, wsl.InodeCommon.id)
	return fuse.OK
}

func (wsl *WorkspaceList) Open(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	return fuse.ENOSYS
}

func (wsl *WorkspaceList) OpenDir(c *ctx, flags uint32,
	mode uint32, out *fuse.OpenOut) fuse.Status {

	ds := newDirectorySnapshot(c, wsl)
	c.qfs.setFileHandle(c, ds.FileHandleCommon.id, ds)
	out.Fh = uint64(ds.FileHandleCommon.id)
	out.OpenFlags = 0

	return fuse.OK
}

func (wsl *WorkspaceList) getChildSnapshot(c *ctx) []directoryContents {
	list, err := c.workspaceDB.WorkspaceList(&c.Ctx, wsl.namespaceName)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	updateChildren(c, wsl.namespaceName, list,
		&wsl.workspacesByName, &wsl.workspacesById, wsl, newWorkspaceRoot)
	children := snapshotChildren(c, &wsl.workspacesByName, fillWorkspaceAttrFake)

	return children
}

func (wsl *WorkspaceList) Lookup(c *ctx, name string,
	out *fuse.EntryOut) fuse.Status {

	exists, err := c.workspaceDB.WorkspaceExists(&c.Ctx, wsl.namespaceName, name)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	if !exists {
		return fuse.ENOENT
	}

	var list []string
	list, err = c.workspaceDB.WorkspaceList(&c.Ctx, wsl.namespaceName)
	assert(err == nil, "BUG: 175630 - handle workspace API errors")

	updateChildren(c, wsl.namespaceName, list,
		&wsl.workspacesByName, &wsl.workspacesById, wsl, newWorkspaceRoot)

	inodeNum := wsl.workspacesByName[name]
	out.NodeId = uint64(inodeNum)
	fillEntryOutCacheData(c, out)
	fillWorkspaceAttrFake(c, &out.Attr, inodeNum, name)

	return fuse.OK
}

func (wsl *WorkspaceList) Create(c *ctx, input *fuse.CreateIn, name string,
	out *fuse.CreateOut) fuse.Status {

	return fuse.EACCES
}

func (wsl *WorkspaceList) SetAttr(c *ctx, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	c.elog("Invalid SetAttr on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) Mkdir(c *ctx, name string, input *fuse.MkdirIn,
	out *fuse.EntryOut) fuse.Status {

	return fuse.EPERM
}

func (wsl *WorkspaceList) getChildRecord(c *ctx,
	inodeNum InodeId) (quantumfs.DirectoryRecord, error) {

	c.elog("Unsupported record fetch on WorkspaceList")
	return quantumfs.DirectoryRecord{},
		errors.New("Unsupported record fetch on WorkspaceList")
}

func (wsl *WorkspaceList) Unlink(c *ctx, name string) fuse.Status {
	c.elog("Invalid Unlink on WorkspaceList")
	return fuse.ENOTDIR
}

func (wsl *WorkspaceList) Rmdir(c *ctx, name string) fuse.Status {
	c.elog("Invalid Rmdir on WorkspaceList")
	return fuse.EACCES
}

func (wsl *WorkspaceList) Symlink(c *ctx, pointedTo string, linkName string,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Symlink on WorkspaceList")
	return fuse.EACCES
}

func (wsl *WorkspaceList) Readlink(c *ctx) ([]byte, fuse.Status) {
	c.elog("Invalid Readlink on WorkspaceList")
	return nil, fuse.EINVAL
}

func (wsl *WorkspaceList) Mknod(c *ctx, name string, input *fuse.MknodIn,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Mknod on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) RenameChild(c *ctx, oldName string,
	newName string) fuse.Status {

	c.elog("Invalid RenameChild on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) MvChild(c *ctx, dstInode Inode, oldName string,
	newName string) fuse.Status {

	c.elog("Invalid MvChild on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) GetXAttrSize(c *ctx,
	attr string) (size int, result fuse.Status) {

	c.elog("Invalid GetXAttrSize on WorkspaceList")
	return 0, fuse.ENODATA
}

func (wsl *WorkspaceList) GetXAttrData(c *ctx,
	attr string) (data []byte, result fuse.Status) {

	c.elog("Invalid GetXAttrData on WorkspaceList")
	return nil, fuse.ENODATA
}

func (wsl *WorkspaceList) ListXAttr(c *ctx) (attributes []byte, result fuse.Status) {
	c.elog("Invalid ListXAttr on WorkspaceList")
	return []byte{}, fuse.OK
}

func (wsl *WorkspaceList) SetXAttr(c *ctx, attr string, data []byte) fuse.Status {
	c.elog("Invalid SetXAttr on WorkspaceList")
	return fuse.Status(syscall.ENOSPC)
}

func (wsl *WorkspaceList) RemoveXAttr(c *ctx, attr string) fuse.Status {
	c.elog("Invalid RemoveXAttr on WorkspaceList")
	return fuse.ENODATA
}

func (wsl *WorkspaceList) syncChild(c *ctx, inodeNum InodeId,
	newKey quantumfs.ObjectKey) {

	c.elog("Invalid syncChild on WorkspaceList")
}

func (wsl *WorkspaceList) setChildAttr(c *ctx, inodeNum InodeId,
	newType *quantumfs.ObjectType, attr *fuse.SetAttrIn,
	out *fuse.AttrOut, updateMtime bool) fuse.Status {

	c.elog("Invalid setChildAttr on WorkspaceList")
	return fuse.ENOSYS
}

func (wsl *WorkspaceList) getChildXAttrSize(c *ctx, inodeNum InodeId,
	attr string) (size int, result fuse.Status) {

	c.elog("Invalid getChildXAttrSize on WorkspaceList")
	return 0, fuse.ENODATA
}

func (wsl *WorkspaceList) getChildXAttrData(c *ctx, inodeNum InodeId,
	attr string) (data []byte, result fuse.Status) {

	c.elog("Invalid getChildXAttrData on WorkspaceList")
	return nil, fuse.ENODATA
}

func (wsl *WorkspaceList) listChildXAttr(c *ctx,
	inodeNum InodeId) (attributes []byte, result fuse.Status) {

	c.elog("Invalid listChildXAttr on WorkspaceList")
	return []byte{}, fuse.OK
}

func (wsl *WorkspaceList) setChildXAttr(c *ctx, inodeNum InodeId, attr string,
	data []byte) fuse.Status {

	c.elog("Invalid setChildXAttr on WorkspaceList")
	return fuse.Status(syscall.ENOSPC)
}

func (wsl *WorkspaceList) removeChildXAttr(c *ctx, inodeNum InodeId,
	attr string) fuse.Status {

	c.elog("Invalid removeChildXAttr on WorkspaceList")
	return fuse.ENODATA
}

func (wsl *WorkspaceList) instantiateChild(c *ctx,
	inodeNum InodeId) (Inode, []InodeId) {

	c.vlog("WorkspaceList::instantiateChild Enter")
	defer c.vlog("WorkspaceList::instantiateChild Exit")

	name, exists := wsl.workspacesById[inodeNum]
	if exists {
		c.vlog("Instantiating %d -> %s/%s", inodeNum, wsl.namespaceName,
			name)
	} else {
		c.vlog("inode %d doesn't exist", inodeNum)
	}

	if wsl.namespaceName == "_null" && wsl.workspacesById[inodeNum] == "null" {
		return newNullWorkspaceRoot(c, wsl.namespaceName,
			wsl.workspacesById[inodeNum], wsl, inodeNum), nil
	} else {
		return newWorkspaceRoot(c, wsl.namespaceName,
			wsl.workspacesById[inodeNum], wsl, inodeNum), nil
	}
}

func (wsl *WorkspaceList) markSelfAccessed(c *ctx, created bool) {
	wsl.markAccessed(c, "", created)
	return
}

func (wsl *WorkspaceList) markAccessed(c *ctx, path string, created bool) {
	c.elog("Invalid markAccessed on WorkspaceList")
	return
}

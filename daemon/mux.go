// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// go-fuse creates a goroutine for every request. The code here simply takes these
// requests and forwards them to the correct Inode.
package daemon

import "math"
import "syscall"
import "sync"
import "sync/atomic"

import "arista.com/quantumfs"
import "arista.com/quantumfs/qlog"
import "github.com/hanwen/go-fuse/fuse"

func NewQuantumFs(config QuantumFsConfig) fuse.RawFileSystem {
	qfs := &QuantumFs{
		RawFileSystem: fuse.NewDefaultRawFileSystem(),
		config:        config,
		inodes:        make(map[uint64]Inode),
		fileHandles:   make(map[uint64]FileHandle),
		inodeNum:      quantumfs.InodeIdReservedEnd,
		fileHandleNum: quantumfs.InodeIdReservedEnd,
		c: ctx{
			config:       &config,
			workspaceDB:  config.WorkspaceDB,
			durableStore: config.DurableStore,
		},
	}

	qfs.c.qfs = qfs

	qfs.inodes[quantumfs.InodeIdRoot] = NewNamespaceList()
	qfs.inodes[quantumfs.InodeIdApi] = NewApiInode()
	return qfs
}

type QuantumFs struct {
	fuse.RawFileSystem
	config        QuantumFsConfig
	inodeNum      uint64
	fileHandleNum uint64
	c             ctx

	mapMutex    sync.Mutex // TODO: Perhaps an RWMutex instead?
	inodes      map[uint64]Inode
	fileHandles map[uint64]FileHandle
}

// Get an inode in a thread safe way
func (qfs *QuantumFs) inode(c *ctx, id uint64) Inode {
	qfs.mapMutex.Lock()
	inode := qfs.inodes[id]
	qfs.mapMutex.Unlock()
	return inode
}

// Set an inode in a thread safe way, set to nil to delete
func (qfs *QuantumFs) setInode(c *ctx, id uint64, inode Inode) {
	qfs.mapMutex.Lock()
	if inode != nil {
		qfs.inodes[id] = inode
	} else {
		delete(qfs.inodes, id)
	}
	qfs.mapMutex.Unlock()
}

// Get a file handle in a thread safe way
func (qfs *QuantumFs) fileHandle(c *ctx, id uint64) FileHandle {
	qfs.mapMutex.Lock()
	fileHandle := qfs.fileHandles[id]
	qfs.mapMutex.Unlock()
	return fileHandle
}

// Set a file handle in a thread safe way, set to nil to delete
func (qfs *QuantumFs) setFileHandle(c *ctx, id uint64, fileHandle FileHandle) {
	qfs.mapMutex.Lock()
	if fileHandle != nil {
		qfs.fileHandles[id] = fileHandle
	} else {
		delete(qfs.fileHandles, id)
	}
	qfs.mapMutex.Unlock()
}

// Retrieve a unique inode number
func (qfs *QuantumFs) newInodeId() uint64 {
	return atomic.AddUint64(&qfs.inodeNum, 1)
}

// Retrieve a unique filehandle number
func (qfs *QuantumFs) newFileHandleId() uint64 {
	return atomic.AddUint64(&qfs.fileHandleNum, 1)
}

func (qfs *QuantumFs) Lookup(header *fuse.InHeader, name string,
	out *fuse.EntryOut) fuse.Status {

	inode := qfs.inode(&qfs.c, header.NodeId)
	c := qfs.c.req(header.Unique)
	if inode == nil {
		c.elog("Lookup failed", name)
		return fuse.ENOENT
	}

	return inode.Lookup(c, header.Context, name, out)
}

func (qfs *QuantumFs) Forget(nodeID uint64, nlookup uint64) {
	qlog.Log(qlog.LogDaemon, qlog.MaxReqId, 2,
		"Forgetting inode %d Looked up %d Times", nodeID, nlookup)
	qfs.setInode(&qfs.c, nodeID, nil)
}

func (qfs *QuantumFs) GetAttr(input *fuse.GetAttrIn, out *fuse.AttrOut) fuse.Status {

	inode := qfs.inode(&qfs.c, input.NodeId)
	if inode == nil {
		return fuse.ENOENT
	}

	return inode.GetAttr(qfs.c.req(input.Unique), out)
}

func (qfs *QuantumFs) SetAttr(input *fuse.SetAttrIn, out *fuse.AttrOut) fuse.Status {
	inode := qfs.inode(&qfs.c, input.NodeId)
	if inode == nil {
		return fuse.ENOENT
	}

	return inode.SetAttr(qfs.c.req(input.Unique), input, out)
}

func (qfs *QuantumFs) Mknod(input *fuse.MknodIn, name string,
	out *fuse.EntryOut) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Mknod")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Mkdir(input *fuse.MkdirIn, name string,
	out *fuse.EntryOut) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Mkdir")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Unlink(header *fuse.InHeader, name string) fuse.Status {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request Unlink")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Rmdir(header *fuse.InHeader, name string) fuse.Status {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request Rmdir")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Rename(input *fuse.RenameIn, oldName string,
	newName string) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Rename")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Link(input *fuse.LinkIn, filename string,
	out *fuse.EntryOut) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Link")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Symlink(header *fuse.InHeader, pointedTo string,
	linkName string, out *fuse.EntryOut) fuse.Status {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request Symlink")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Readlink(header *fuse.InHeader) (out []byte,
	code fuse.Status) {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request Readlink")
	return nil, fuse.ENOSYS
}

func (qfs *QuantumFs) Access(input *fuse.AccessIn) fuse.Status {
	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Access")
	return fuse.OK
}

func (qfs *QuantumFs) GetXAttrSize(header *fuse.InHeader, attr string) (sz int,
	code fuse.Status) {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request GetXAttrSize")
	return 0, fuse.ENOSYS
}

func (qfs *QuantumFs) GetXAttrData(header *fuse.InHeader, attr string) (data []byte,
	code fuse.Status) {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request GetXAttrData")
	return nil, fuse.ENOSYS
}

func (qfs *QuantumFs) ListXAttr(header *fuse.InHeader) (attributes []byte,
	code fuse.Status) {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request ListXAttr")
	return nil, fuse.ENOSYS
}

func (qfs *QuantumFs) SetXAttr(input *fuse.SetXAttrIn, attr string,
	data []byte) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request SetXAttr")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) RemoveXAttr(header *fuse.InHeader, attr string) fuse.Status {

	c := qfs.c.req(header.Unique)
	c.elog("Unhandled request RemoveXAttr")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Create(input *fuse.CreateIn, name string,
	out *fuse.CreateOut) fuse.Status {

	inode := qfs.inode(&qfs.c, input.NodeId)
	c := qfs.c.req(input.Unique)
	if inode == nil {
		c.elog("Create failed", input)
		return fuse.EACCES // TODO Confirm this is correct
	}

	return inode.Create(c, input, name, out)
}

func (qfs *QuantumFs) Open(input *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	inode := qfs.inode(&qfs.c, input.NodeId)
	c := qfs.c.req(input.Unique)
	if inode == nil {
		c.elog("Open failed", input)
		return fuse.ENOENT
	}

	return inode.Open(c, input.Flags, input.Mode, out)
}

func (qfs *QuantumFs) Read(input *fuse.ReadIn, buf []byte) (fuse.ReadResult,
	fuse.Status) {

	c := qfs.c.req(input.Unique)
	c.elog("Read:", input)
	fileHandle := qfs.fileHandle(&qfs.c, input.Fh)
	if fileHandle == nil {
		c.elog("Read failed", fileHandle)
		return nil, fuse.ENOENT
	}
	return fileHandle.Read(c, input.Offset, input.Size,
		buf, BitFlagsSet(uint(input.Flags), uint(syscall.O_NONBLOCK)))
}

func (qfs *QuantumFs) Release(input *fuse.ReleaseIn) {
	qfs.setFileHandle(&qfs.c, input.Fh, nil)
}

func (qfs *QuantumFs) Write(input *fuse.WriteIn, data []byte) (uint32, fuse.Status) {
	fileHandle := qfs.fileHandle(&qfs.c, input.Fh)
	c := qfs.c.req(input.Unique)
	if fileHandle == nil {
		c.elog("Write failed", fileHandle)
		return 0, fuse.ENOENT
	}
	return fileHandle.Write(c, input.Offset, input.Size,
		input.Flags, data)
}

func (qfs *QuantumFs) Flush(input *fuse.FlushIn) fuse.Status {
	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Flush")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Fsync(input *fuse.FsyncIn) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Fsync")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) Fallocate(input *fuse.FallocateIn) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request Fallocate")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) OpenDir(input *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	inode := qfs.inode(&qfs.c, input.NodeId)
	c := qfs.c.req(input.Unique)
	if inode == nil {
		c.elog("OpenDir failed", input)
		return fuse.ENOENT
	}

	return inode.OpenDir(c, input.InHeader.Context,
		input.Flags, input.Mode, out)
}

func (qfs *QuantumFs) ReadDir(input *fuse.ReadIn,
	out *fuse.DirEntryList) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request ReadDir")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) ReadDirPlus(input *fuse.ReadIn,
	out *fuse.DirEntryList) fuse.Status {

	fileHandle := qfs.fileHandle(&qfs.c, input.Fh)
	c := qfs.c.req(input.Unique)
	if fileHandle == nil {
		c.elog("ReadDirPlus failed", fileHandle)
		return fuse.ENOENT
	}
	return fileHandle.ReadDirPlus(c, input, out)
}

func (qfs *QuantumFs) ReleaseDir(input *fuse.ReleaseIn) {
	qfs.setFileHandle(&qfs.c, input.Fh, nil)
}

func (qfs *QuantumFs) FsyncDir(input *fuse.FsyncIn) fuse.Status {

	c := qfs.c.req(input.Unique)
	c.elog("Unhandled request FsyncDir")
	return fuse.ENOSYS
}

func (qfs *QuantumFs) StatFs(input *fuse.InHeader, out *fuse.StatfsOut) fuse.Status {
	out.Blocks = 2684354560 // 10TB
	out.Bfree = out.Blocks / 2
	out.Bavail = out.Bfree
	out.Files = 0
	out.Ffree = math.MaxUint64
	out.Bsize = qfsBlockSize
	out.NameLen = quantumfs.MaxFilenameLength
	out.Frsize = 0

	return fuse.OK
}

func (qfs *QuantumFs) Init(*fuse.Server) {
	qlog.Log(qlog.LogDaemon, qlog.MaxReqId, 0, "Unhandled request Init")
}
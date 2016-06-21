// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// This file holds the File type, which represents regular files

import "arista.com/quantumfs"
import "errors"
import "github.com/hanwen/go-fuse/fuse"
import "syscall"

const execBit = 0x1
const writeBit = 0x2
const readBit = 0x4

func newSmallFile(c *ctx, key quantumfs.ObjectKey, size uint64, inodeNum InodeId,
	parent Inode) Inode {

	accessor := newSmallAccessor(c, size, key)

	return newFile_(c, quantumfs.ObjectTypeSmallFile, inodeNum, key, parent,
		accessor)
}

func newMediumFile(c *ctx, key quantumfs.ObjectKey, size uint64, inodeNum InodeId,
	parent Inode) Inode {

	accessor := newMediumAccessor(c, key)

	return newFile_(c, quantumfs.ObjectTypeMediumFile, inodeNum, key, parent,
		accessor)
}

func newFile_(c *ctx, fileType quantumfs.ObjectType, inodeNum InodeId,
	key quantumfs.ObjectKey, parent Inode, accessor blockAccessor) *File {

	file := File{
		InodeCommon: InodeCommon{id: inodeNum},
		fileType:    fileType,
		key:         key,
		parent:      parent,
		accessor:    accessor,
	}
	file.self = &file

	return &file
}

type File struct {
	InodeCommon
	fileType quantumfs.ObjectType
	key      quantumfs.ObjectKey
	parent   Inode
	accessor blockAccessor
}

// Mark this file dirty and notify your paent
func (fi *File) dirty(c *ctx) {
	fi.setDirty(true)
	fi.parent.dirtyChild(c, fi)
}

func (fi *File) sync(c *ctx) quantumfs.ObjectKey {
	fi.setDirty(false)
	return fi.key
}

func (fi *File) Access(c *ctx, mask uint32, uid uint32,
	gid uint32) fuse.Status {

	c.elog("Unsupported Access on File")
	return fuse.ENOSYS
}

func (fi *File) GetAttr(c *ctx, out *fuse.AttrOut) fuse.Status {
	record, err := fi.parent.getChildRecord(c, fi.InodeCommon.id)
	if err != nil {
		c.elog("Unable to get record from parent for inode %d", fi.id)
		return fuse.EIO
	}

	fillAttrOutCacheData(c, out)
	fillAttrWithDirectoryRecord(c, &out.Attr, fi.InodeCommon.id, c.fuseCtx.Owner,
		&record)

	return fuse.OK
}

func (fi *File) OpenDir(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	return fuse.ENOTDIR
}

func (fi *File) openPermission(c *ctx, flags uint32) bool {
	record, error := fi.parent.getChildRecord(c, fi.id)
	if error != nil {
		return false
	}

	c.vlog("Open permission check. Have %x, flags %x", record.Permissions, flags)
	//this only works because we don't have owner/group/other specific perms.
	//we need to confirm whether we can treat the root user/group specially.
	switch flags & syscall.O_ACCMODE {
	case syscall.O_RDONLY:
		return (record.Permissions & readBit) != 0
	case syscall.O_WRONLY:
		return (record.Permissions & writeBit) != 0
	case syscall.O_RDWR:
		var bitmask uint8 = readBit | writeBit
		return (record.Permissions & bitmask) == bitmask
	}

	return false
}

func (fi *File) Open(c *ctx, flags uint32, mode uint32,
	out *fuse.OpenOut) fuse.Status {

	if !fi.openPermission(c, flags) {
		return fuse.EPERM
	}

	fileHandleNum := c.qfs.newFileHandleId()
	fileDescriptor := newFileDescriptor(fi, fi.id, fileHandleNum)
	c.qfs.setFileHandle(c, fileHandleNum, fileDescriptor)

	out.OpenFlags = 0
	out.Fh = uint64(fileHandleNum)

	return fuse.OK
}

func (fi *File) Lookup(c *ctx, name string, out *fuse.EntryOut) fuse.Status {
	return fuse.ENOSYS
}

func (fi *File) Create(c *ctx, input *fuse.CreateIn, name string,
	out *fuse.CreateOut) fuse.Status {

	return fuse.ENOTDIR
}

func (fi *File) SetAttr(c *ctx, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	result := func() fuse.Status {
		fi.lock.Lock()
		defer fi.lock.Unlock()

		if BitFlagsSet(uint(attr.Valid), fuse.FATTR_SIZE) {
			endBlkIdx, _ := fi.accessor.blockIdxInfo(attr.Size)

			err := fi.reconcileFileType(c, endBlkIdx)
			if err != nil {
				c.elog("Could not reconcile file type with new end" +
					" blockIdx")
				return fuse.EIO
			}

			err = fi.accessor.truncate(c, uint64(attr.Size))
			if err != nil {
				return fuse.EIO
			}
		}

		// Update the entry metadata
		fi.key = fi.accessor.writeToStore(c)

		return fuse.OK
	}()

	if result != fuse.OK {
		return result
	}

	return fi.parent.setChildAttr(c, fi.InodeCommon.id, attr, out)
}

func (fi *File) Mkdir(c *ctx, name string, input *fuse.MkdirIn,
	out *fuse.EntryOut) fuse.Status {

	return fuse.ENOTDIR
}

func (fi *File) Unlink(c *ctx, name string) fuse.Status {
	c.elog("Invalid Unlink on File")
	return fuse.ENOTDIR
}

func (fi *File) Rmdir(c *ctx, name string) fuse.Status {
	c.elog("Invalid Rmdir on File")
	return fuse.ENOTDIR
}

func (fi *File) Symlink(c *ctx, pointedTo string, linkName string,
	out *fuse.EntryOut) fuse.Status {

	c.elog("Invalid Symlink on File")
	return fuse.ENOTDIR
}

func (fi *File) Readlink(c *ctx) ([]byte, fuse.Status) {
	c.elog("Invalid Readlink on File")
	return nil, fuse.EINVAL
}

func (fi *File) setChildAttr(c *ctx, inodeNum InodeId, attr *fuse.SetAttrIn,
	out *fuse.AttrOut) fuse.Status {

	c.elog("Invalid setChildAttr on File")
	return fuse.ENOSYS
}

func (fi *File) getChildRecord(c *ctx, inodeNum InodeId) (quantumfs.DirectoryRecord,
	error) {

	c.elog("Unsupported record fetch on file")
	return quantumfs.DirectoryRecord{}, errors.New("Unsupported record fetch")
}

func resize(buffer []byte, size int) []byte {
	if len(buffer) > size {
		return buffer[:size]
	}

	for len(buffer) < size {
		newLength := make([]byte, size-len(buffer))
		buffer = append(buffer, newLength...)
	}

	return buffer
}

func fetchDataSized(c *ctx, key quantumfs.ObjectKey,
	targetSize int) *quantumfs.Buffer {

	rtn := DataStore.Get(c, key)
	if rtn == nil {
		c.elog("Data for key missing from datastore")
		return nil
	}

	// Before we return the buffer, make sure it's the size it needs to be
	rtn.Set(resize(rtn.Get(), targetSize))

	return rtn
}

func pushData(c *ctx, buffer *quantumfs.Buffer) (*quantumfs.ObjectKey,
	error) {

	newFileKey := buffer.Key(quantumfs.KeyTypeData)

	err := DataStore.Set(c, newFileKey,
		quantumfs.NewBuffer(buffer.Get()))
	if err != nil {
		c.elog("Unable to write data to the datastore")
		return nil, errors.New("Unable to write to the datastore")
	}

	return &newFileKey, nil
}

func calcTypeGivenBlocks(numBlocks int) quantumfs.ObjectType {
	switch {
	case numBlocks <= 1:
		return quantumfs.ObjectTypeSmallFile
	case numBlocks <= quantumfs.MaxBlocksMediumFile:
		return quantumfs.ObjectTypeMediumFile
	case numBlocks <= quantumfs.MaxBlocksLargeFile:
		return quantumfs.ObjectTypeLargeFile
	default:
		return quantumfs.ObjectTypeVeryLargeFile
	}
}

// Given the block index to write into the file, ensure that we are the
// correct file type
func (fi *File) reconcileFileType(c *ctx, blockIdx int) error {

	neededType := calcTypeGivenBlocks(blockIdx + 1)
	newAccessor := fi.accessor.convertTo(c, neededType)
	if newAccessor == nil {
		return errors.New("Unable to process needed type for accessor")
	}

	fi.accessor = newAccessor
	return nil
}

type blockAccessor interface {

	// Read data from the block via an index
	readBlock(*ctx, int, uint64, []byte) (int, error)

	// Write data to a block via an index
	writeBlock(*ctx, int, uint64, []byte) (int, error)

	// Get the file's length in bytes
	fileLength() uint64

	// Extract block and remaining offset from absolute offset
	blockIdxInfo(uint64) (int, uint64)

	// Convert contents into new accessor type, nil accessor if current is fine
	convertTo(*ctx, quantumfs.ObjectType) blockAccessor

	// Write file's metadata to the datastore and provide the key
	writeToStore(c *ctx) quantumfs.ObjectKey

	// Truncate to lessen length *only*, error otherwise
	truncate(*ctx, uint64) error
}

func (fi *File) writeBlock(c *ctx, blockIdx int, offset uint64, buf []byte) (int,
	error) {

	err := fi.reconcileFileType(c, blockIdx)
	if err != nil {
		c.elog("Could not reconcile file type with new blockIdx")
		return 0, err
	}

	var written int
	written, err = fi.accessor.writeBlock(c, blockIdx, offset, buf)
	if err != nil {
		return 0, err
	}

	return written, nil
}

type blockFn func(*ctx, int, uint64, []byte) (int, error)

// Returns the number of bytes operated on, and any error code
func (fi *File) operateOnBlocks(c *ctx, offset uint64, size uint32, buf []byte,
	fn blockFn) (uint64, error) {

	count := uint64(0)

	// Ensure size and buf are consistent
	buf = buf[:size]
	size = uint32(len(buf))

	if size == 0 {
		c.vlog("block operation with zero size or buf")
		return 0, nil
	}

	// Determine the block to start in
	startBlkIdx, newOffset := fi.accessor.blockIdxInfo(offset)
	endBlkIdx, _ := fi.accessor.blockIdxInfo(offset + uint64(size))
	offset = newOffset

	// Handle the first block a little specially (with offset)
	iterCount, err := fn(c, startBlkIdx, offset, buf[count:])
	if err != nil {
		c.elog("Unable to operate on first data block")
		return 0, errors.New("Unable to operate on first data block")
	}
	count += uint64(iterCount)

	// Loop through the blocks, operating on them
	for i := startBlkIdx + 1; i <= endBlkIdx; i++ {
		iterCount, err = fn(c, i, 0, buf[count:])
		if err != nil {
			// We couldn't do more, but that's okay we've done some
			// already so just return early and report what we've done
			break
		}
		count += uint64(iterCount)
	}

	return count, nil
}

func (fi *File) Read(c *ctx, offset uint64, size uint32, buf []byte,
	nonblocking bool) (fuse.ReadResult, fuse.Status) {

	fi.lock.RLock()
	defer fi.lock.RUnlock()

	readCount, err := fi.operateOnBlocks(c, offset, size, buf,
		fi.accessor.readBlock)

	if err != nil {
		return fuse.ReadResult(nil), fuse.EIO
	}

	return fuse.ReadResultData(buf[:readCount]), fuse.OK
}

func (fi *File) Write(c *ctx, offset uint64, size uint32, flags uint32,
	buf []byte) (uint32, fuse.Status) {

	writeCount, result := func() (uint32, fuse.Status) {
		fi.lock.Lock()
		defer fi.lock.Unlock()

		writeCount, err := fi.operateOnBlocks(c, offset, size, buf,
			fi.writeBlock)

		if err != nil {
			return 0, fuse.EIO
		}

		// Update the direct entry
		fi.key = fi.accessor.writeToStore(c)
		return uint32(writeCount), fuse.OK
	}()

	if result != fuse.OK {
		return writeCount, result
	}

	// Update the size with what we were able to write
	var attr fuse.SetAttrIn
	attr.Valid = fuse.FATTR_SIZE
	attr.Size = uint64(fi.accessor.fileLength())
	fi.parent.setChildAttr(c, fi.id, &attr, nil)
	fi.dirty(c)

	return writeCount, fuse.OK
}

func newFileDescriptor(file *File, inodeNum InodeId,
	fileHandleId FileHandleId) FileHandle {

	return &FileDescriptor{
		FileHandleCommon: FileHandleCommon{
			id:       fileHandleId,
			inodeNum: inodeNum,
		},
		file: file,
	}
}

type FileDescriptor struct {
	FileHandleCommon
	file *File
}

func (fd *FileDescriptor) dirty(c *ctx) {
	fd.file.self.dirty(c)
}

func (fd *FileDescriptor) ReadDirPlus(c *ctx, input *fuse.ReadIn,
	out *fuse.DirEntryList) fuse.Status {

	c.elog("Invalid ReadDirPlus against FileDescriptor")
	return fuse.ENOSYS
}

func (fd *FileDescriptor) Read(c *ctx, offset uint64, size uint32, buf []byte,
	nonblocking bool) (fuse.ReadResult, fuse.Status) {

	return fd.file.Read(c, offset, size, buf, nonblocking)
}

func (fd *FileDescriptor) Write(c *ctx, offset uint64, size uint32, flags uint32,
	buf []byte) (uint32, fuse.Status) {

	return fd.file.Write(c, offset, size, flags, buf)
}

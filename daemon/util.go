// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package daemon

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/utils"
	"github.com/hanwen/go-fuse/fuse"
)

const R_OK = 4
const W_OK = 2
const X_OK = 1
const F_OK = 0

func modifyEntryWithAttr(c *ctx, newType *quantumfs.ObjectType, attr *fuse.SetAttrIn,
	entry quantumfs.DirectoryRecord, updateMtime bool) {

	defer c.FuncIn("modifyEntryWithAttr", "valid %x", attr.Valid).Out()

	// Update the type if needed
	if newType != nil {
		entry.SetType(*newType)
		c.vlog("Type now %d", *newType)
	}

	valid := uint(attr.SetAttrInCommon.Valid)
	// We don't support file locks yet, but when we do we need
	// FATTR_LOCKOWNER

	var now quantumfs.Time
	if utils.BitAnyFlagSet(valid, fuse.FATTR_MTIME_NOW) ||
		!utils.BitFlagsSet(valid, fuse.FATTR_CTIME) || updateMtime {

		now = quantumfs.NewTime(time.Now())
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_MODE) {
		entry.SetPermissions(modeToPermissions(attr.Mode, 0))
		c.vlog("Permissions now %d Mode %d", entry.Permissions(), attr.Mode)
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_UID) {
		entry.SetOwner(quantumfs.ObjectUid(attr.Owner.Uid,
			c.fuseCtx.Owner.Uid))
		c.vlog("Owner now %d UID %d context %d", entry.Owner(),
			attr.Owner.Uid, c.fuseCtx.Owner.Uid)
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_GID) {
		entry.SetGroup(quantumfs.ObjectGid(attr.Owner.Gid,
			c.fuseCtx.Owner.Gid))
		c.vlog("Group now %d GID %d context %d", entry.Group(),
			attr.Owner.Gid, c.fuseCtx.Owner.Gid)
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_SIZE) {
		entry.SetSize(attr.Size)
		c.vlog("Size now %d", entry.Size())
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_ATIME|fuse.FATTR_ATIME_NOW) {
		// atime is ignored and not stored
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_MTIME_NOW) {
		entry.SetModificationTime(now)
		c.vlog("ModificationTime now %d", entry.ModificationTime())
	} else if utils.BitFlagsSet(valid, fuse.FATTR_MTIME) {
		entry.SetModificationTime(
			quantumfs.NewTimeSeconds(attr.Mtime, attr.Mtimensec))
		c.vlog("ModificationTime now %d", entry.ModificationTime())
	} else if updateMtime {
		c.vlog("Updated mtime")
		entry.SetModificationTime(now)
	}

	if utils.BitFlagsSet(valid, fuse.FATTR_CTIME) {
		entry.SetContentTime(quantumfs.NewTimeSeconds(attr.Ctime,
			attr.Ctimensec))
		c.vlog("ContentTime now %d", entry.ContentTime())
	} else {
		// Since we've updated the file attributes we need to update at least
		// its ctime (unless we've explicitly set its ctime).
		c.vlog("Updated ctime")
		entry.SetContentTime(now)
	}
}

// Return the fuse connection id for the filesystem mounted at the given path
func findFuseConnection(c *ctx, mountPath string) int {
	defer c.FuncIn("findFuseConnection", "mountPath %s", mountPath).Out()
	c.dlog("Finding FUSE Connection ID...")
	for i := 0; i < 100; i++ {
		c.dlog("Waiting for mount try %d...", i)
		file, err := os.Open("/proc/self/mountinfo")
		if err != nil {
			c.dlog("Failed opening mountinfo: %s", err.Error())
			return -1
		}
		defer file.Close()

		mountinfo := bufio.NewReader(file)

		for {
			bline, _, err := mountinfo.ReadLine()
			if err != nil {
				break
			}

			line := string(bline)

			if strings.Contains(line, mountPath) {
				fields := strings.SplitN(line, " ", 5)
				dev := strings.Split(fields[2], ":")[1]
				devInt, err := strconv.Atoi(dev)
				if err != nil {
					c.elog("Failed to convert dev to integer")
					return -1
				}
				c.vlog("Found mountId %d", devInt)
				return devInt
			}
		}

		time.Sleep(50 * time.Millisecond)
	}
	c.elog("FUSE mount not found in time")
	return -1
}

func hasAccessPermission(c *ctx, inode Inode, mode uint32, uid uint32,
	gid uint32) fuse.Status {

	// translate access flags into permission flags and return the result
	var checkFlags uint32
	if mode&R_OK != 0 {
		checkFlags |= quantumfs.PermReadAll
	}

	if mode&W_OK != 0 {
		checkFlags |= quantumfs.PermWriteAll
	}

	if mode&X_OK != 0 {
		checkFlags |= quantumfs.PermExecAll
	}

	pid := c.fuseCtx.Pid

	defer inode.parentRLock(c).RUnlock()
	return hasPermissionIds_(c, inode, uid, gid, pid, checkFlags, -1)
}

// Must be called with the parentLock
func hasDirectoryWritePermSticky_(c *ctx, inode Inode,
	childOwner quantumfs.UID) fuse.Status {

	if c.fuseCtx == nil {
		return fuse.OK
	}
	checkFlags := uint32(quantumfs.PermWriteAll | quantumfs.PermExecAll)
	owner := c.fuseCtx.Owner
	pid := c.fuseCtx.Pid
	return hasPermissionIds_(c, inode, owner.Uid, owner.Gid, pid, checkFlags,
		int32(childOwner))
}

// Must be called with the parentLock
func hasDirectoryWritePerm_(c *ctx, inode Inode) fuse.Status {
	// Directories require execute permission in order to traverse them.
	// So, we must check both write and execute bits

	if c.fuseCtx == nil {
		return fuse.OK
	}
	checkFlags := uint32(quantumfs.PermWriteAll | quantumfs.PermExecAll)
	owner := c.fuseCtx.Owner
	pid := c.fuseCtx.Pid
	return hasPermissionIds_(c, inode, owner.Uid, owner.Gid, pid, checkFlags, -1)
}

func hasPermissionOpenFlags(c *ctx, inode Inode, openFlags uint32) fuse.Status {

	// convert open flags into permission ones
	checkFlags := uint32(0)
	switch openFlags & syscall.O_ACCMODE {
	case syscall.O_RDONLY:
		checkFlags = quantumfs.PermReadAll
	case syscall.O_WRONLY:
		checkFlags = quantumfs.PermWriteAll
	case syscall.O_RDWR:
		checkFlags = quantumfs.PermReadAll | quantumfs.PermWriteAll
	}

	if utils.BitFlagsSet(uint(openFlags), FMODE_EXEC) {
		checkFlags |= quantumfs.PermExecAll | quantumfs.PermSUID |
			quantumfs.PermSGID
	}

	owner := c.fuseCtx.Owner
	pid := c.fuseCtx.Pid

	defer inode.parentRLock(c).RUnlock()
	return hasPermissionIds_(c, inode, owner.Uid, owner.Gid, pid, checkFlags, -1)
}

// Determine if the process has a matching group. Normally the primary group is all
// we need to check, but sometimes we also much check the supplementary groups.
func hasMatchingGid(c *ctx, userGid uint32, pid uint32, inodeGid uint32) bool {
	defer c.FuncIn("hasMatchingGid",
		"user gid %d pid %d inode gid %d", userGid, pid, inodeGid).Out()

	// First check the common case where we do the least work
	if userGid == inodeGid {
		c.vlog("user GID matches inode")
		return true
	}

	// The primary group doesn't match. We now need to check the supplementary
	// groups. Unfortunately FUSE doesn't give us these so we need to parse them
	// ourselves out of /proc.
	fd, err := syscall.Open(fmt.Sprintf("/proc/%d/task/%d/status", pid, pid),
		syscall.O_RDONLY, 0)
	if err != nil {
		c.dlog("Unable to open /proc/status for %d: %s", pid, err.Error())
		return false
	}
	defer syscall.Close(fd)

	// We are looking for the Group: line in the status file. This is generally
	// within 4k of the start of the file. Allow extra room to be sure we get it
	// in a single read.
	procStatus := make([]byte, 6000)
	numRead, err := syscall.Read(fd, procStatus)
	if err != nil {
		c.dlog("Failed reading status file for pid %d", pid)
		return false
	}
	procStatus = procStatus[:numRead]

	// Find "Groups:" line
	start := bytes.Index(procStatus, []byte("Groups:\t"))
	if start == -1 {
		c.dlog("Groups: not found in status file for pid %d", pid)
		return false
	}
	procStatus = procStatus[start:]
	//c.vlog("line start '%s'", string(procStatus[:50]))

	// Skip "Groups:\t"
	start = bytes.IndexRune(procStatus, '\t')
	if start == -1 {
		c.dlog("tab not found in groups for pid %d", pid)
		return false
	}
	procStatus = procStatus[start+1:]
	//c.vlog("groups start '%s'", string(procStatus[:50]))

	// Test each number in the line against the group we are looking for
	for {
		nextSeparator := bytes.IndexAny(procStatus, " \n")
		if nextSeparator == -1 {
			c.dlog("reached EOF without Groups newline for pid %d", pid)
			return false
		}

		if nextSeparator == 0 {
			c.vlog("No supplementary groups")
			return false
		}

		num := string(procStatus[:nextSeparator])
		//c.vlog("Parsing '%s'", num)

		gid, err := strconv.Atoi(num)
		if err != nil {
			c.elog("Failed to parse gid from '%s' out of '%s...'",
				num, string(procStatus[:200]))
		} else if uint32(gid) == inodeGid {
			c.vlog("Supplementary group %d matches inode", gid)
			return true
		}

		if procStatus[nextSeparator+1] == '\n' {
			// We reached the end of the line without finding a match
			c.vlog("Reached EOL")
			return false
		}

		//c.vlog("next is '%d'", procStatus[nextSeparator])
		procStatus = procStatus[nextSeparator+1:]
	}
}

// parentLock must be held
func hasPermissionIds_(c *ctx, inode Inode, checkUid uint32,
	checkGid uint32, pid uint32, checkFlags uint32,
	stickyAltOwner int32) fuse.Status {

	if !c.config.MagicOwnership {
		// Assume the kernel has already checked permissions for us
		return fuse.OK
	}

	defer c.FuncIn("hasPermissionIds_", "%d %d %d %d %o", checkUid, checkGid,
		pid, stickyAltOwner, checkFlags).Out()

	// If the inode is a workspace root, it is always permitted to modify the
	// children inodes because its permission is 777 (Hardcoded in
	// daemon/workspaceroot.go).
	if inode.isWorkspaceRoot() {
		c.vlog("Is WorkspaceRoot: OK")
		return fuse.OK
	}

	var attr fuse.Attr
	owner := fuse.Owner{Uid: checkUid, Gid: checkGid}
	inode.parentGetChildAttr_(c, inode.inodeNum(), &attr, owner)
	inodeOwner := attr.Owner.Uid
	inodeGroup := attr.Owner.Gid
	permission := attr.Mode
	isDir := utils.BitFlagsSet(uint(attr.Mode), fuse.S_IFDIR)

	if checkUid == 0 {
		var permMask uint32

		permMask = quantumfs.PermExecOwner |
			quantumfs.PermExecGroup |
			quantumfs.PermExecOther

		if checkFlags&permMask == 0 || isDir {
			// The root user has 'rw' access to regular files and 'rwx'
			// access to directories irrespective of their permissions.
			c.vlog("User is root: OK")
			return fuse.OK
		}
		if permission&permMask == 0 {
			return fuse.EACCES
		}
		c.vlog("User is root: OK")
		return fuse.OK
	}

	// Verify the permission of the inode in order to delete a child
	// If the sticky bit of a directory is set, the action can only be
	// performed by file's owner, directory's owner, or root user
	if stickyAltOwner >= 0 {
		stickyUid := quantumfs.SystemUid(quantumfs.UID(stickyAltOwner),
			checkUid)

		if isDir &&
			utils.BitFlagsSet(uint(permission), quantumfs.PermSticky) &&
			checkUid != inodeOwner && checkUid != stickyUid {

			c.vlog("Sticky owners don't match: FAIL")
			return fuse.EACCES
		}
	}

	// Get whether current user is OWNER/GRP/OTHER
	var permMask uint32
	if checkUid == inodeOwner {
		permMask = quantumfs.PermReadOwner | quantumfs.PermWriteOwner |
			quantumfs.PermExecOwner
	} else if hasMatchingGid(c, checkGid, pid, inodeGroup) {
		permMask = quantumfs.PermReadGroup | quantumfs.PermWriteGroup |
			quantumfs.PermExecGroup
	} else { // all the other
		permMask = quantumfs.PermReadOther | quantumfs.PermWriteOther |
			quantumfs.PermExecOther
	}

	if utils.BitFlagsSet(uint(permission), uint(checkFlags&permMask)) {
		c.vlog("Has permission: OK. %o %o %o", checkFlags, permMask,
			permission)
		return fuse.OK
	}

	// If execute permissions are lacking, but the file has SUID/SGID, then we
	// allow it. This may not be correct behavior, but it's what we've been doing
	if utils.BitAnyFlagSet(uint(permission), uint(quantumfs.PermSUID|
		quantumfs.PermSGID&checkFlags)) {

		c.vlog("SUID/SGID set, Permission OK")
		return fuse.OK
	}

	c.vlog("hasPermissionIds_ (%o & %o) vs %o", checkFlags, permMask, permission)
	return fuse.EACCES
}

func asDirectory(inode Inode) *Directory {
	switch v := inode.(type) {
	case *WorkspaceRoot:
		return &v.Directory
	case *Directory:
		return v
	default:
		// panic like usual
		panic(fmt.Sprintf("Inode %d is not a Directory", inode.inodeNum()))
	}
}

// Notify the kernel that the requested entry isn't found and further that the kernel
// should cache that fact.
func kernelCacheNegativeEntry(c *ctx, out *fuse.EntryOut) fuse.Status {
	// The FUSE API for notifying the kernel of a negative lookup is by returning
	// success with a zero NodeId. See struct fuse_entry_param in libfuse.
	fillEntryOutCacheData(c, out)
	out.NodeId = 0
	out.Generation = 0
	return fuse.OK
}

// Amend the pathFlags with quantumfs.PathIsDir if the type is
// quantumfs.ObjectTypeDirectory, otherwise pass pathFlags through untouched.
func markType(type_ quantumfs.ObjectType,
	pathFlags quantumfs.PathFlags) quantumfs.PathFlags {

	if type_ == quantumfs.ObjectTypeDirectory {
		pathFlags |= quantumfs.PathIsDir
	}

	return pathFlags
}

func underlyingTypeOf(hardlinkTable HardlinkTable,
	record quantumfs.ImmutableDirectoryRecord) quantumfs.ObjectType {

	if record.Type() != quantumfs.ObjectTypeHardlink {
		return record.Type()
	}
	fileId := record.FileId()
	hardlinkRecord := hardlinkTable.recordByFileId(fileId)
	utils.Assert(hardlinkTable != nil, "hardlink %d not found", fileId)
	utils.Assert(hardlinkRecord.Type() != quantumfs.ObjectTypeHardlink,
		"The underlying type cannot be hardlink")
	return hardlinkRecord.Type()
}

func underlyingTypesMatch(hardlinkTable HardlinkTable,
	r1 quantumfs.ImmutableDirectoryRecord,
	r2 quantumfs.ImmutableDirectoryRecord) bool {

	return underlyingTypeOf(hardlinkTable, r1).Matches(
		underlyingTypeOf(hardlinkTable, r2))
}

type publishFn func(*ctx, ImmutableBuffer) (quantumfs.ObjectKey, error)

func publishNow(c *ctx, buf ImmutableBuffer) (quantumfs.ObjectKey, error) {
	return buf.Key(&c.Ctx)
}

// this is for ensuring that a given function is only called once via the struct
type callOnceHandle struct {
	fn func()
}

func callOnce(fn func()) *callOnceHandle {
	return &callOnceHandle{
		fn: fn,
	}
}

func (c *callOnceHandle) invoke() {
	if c.fn == nil {
		return
	}

	c.fn()
	c.fn = nil
}

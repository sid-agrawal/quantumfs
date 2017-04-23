// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

import "fmt"
import "github.com/aristanetworks/quantumfs"
import "github.com/hanwen/go-fuse/fuse"

// Handles map coordination and partial map pairing (for hardlinks) since now the
// mapping between maps isn't one-to-one.
type ChildMap struct {
	wsr *WorkspaceRoot

	// can be many to one
	children map[string]InodeId

	childrenRecords thinChildren
}

func newChildMap(c *ctx, key quantumfs.ObjectKey, wsr_ *WorkspaceRoot) (*ChildMap,
	[]InodeId) {

	rtn := ChildMap{
		wsr:      		wsr_,
		children: 		make(map[string]InodeId),
		childrenRecords:	newThinChildren(key, wsr_),
	}

	// allocate inode ids
	uninstantiated := make([]InodeId, 0)
	rtn.childrenRecords.iterateOverRecords(c,
		func (record quantumfs.DirectoryRecord) bool {

		inodeId := rtn.loadInodeId(c, record, quantumfs.InodeIdInvalid)
		rtn.children[record.Filename()] = inodeId
		uninstantiated = append(uninstantiated, inodeId)
		return false
	})

	return &rtn, uninstantiated
}

// Whenever a record passes through this class, we must ensure it's converted if
// necessary
func convertRecord(wsr *WorkspaceRoot,
	entry quantumfs.DirectoryRecord) quantumfs.DirectoryRecord {

	if entry.Type() == quantumfs.ObjectTypeHardlink {
		linkId := decodeHardlinkKey(entry.ID())
		entry = newHardlink(entry.Filename(), linkId, wsr)
	}

	return entry
}

func (cmap *ChildMap) recordByName(c *ctx, name string) quantumfs.DirectoryRecord {
	defer c.FuncIn("ChildMap::recordByName", "%s", name).out()

	// Do everything we can to optimize this function and allow fast escape
	if _, exists := cmap.children[name]; !exists {
		return nil
	}

	if record, exists := cmap.childrenRecords.changes[name]; exists {
		return record
	}

	var rtn quantumfs.DirectoryRecord
	cmap.childrenRecords.iterateOverRecords(c,
		func (record quantumfs.DirectoryRecord) bool {

		if record.Filename() == name {
			rtn = record
			return true
		}

		return false
	})

	return rtn
}

// Returns the inodeId used for the child
func (cmap *ChildMap) loadInodeId(c *ctx, entry quantumfs.DirectoryRecord,
	inodeId InodeId) InodeId {

	defer c.FuncIn("ChildMap::loadInodeId", "inode %d", inodeId).out()

	if entry.Type() == quantumfs.ObjectTypeHardlink {
		linkId := decodeHardlinkKey(entry.ID())
		establishedInodeId := cmap.wsr.getHardlinkInodeId(c, linkId)

		// If you try to load a hardlink and provide a real inodeId, it
		// should match the actual inodeId for the hardlink or else
		// something is really wrong in the system
		if inodeId != quantumfs.InodeIdInvalid &&
			inodeId != establishedInodeId {

			c.elog("Attempt to set hardlink with mismatched inodeId, "+
				"%d vs %d", inodeId, establishedInodeId)
		}
		inodeId = establishedInodeId
	} else if inodeId == quantumfs.InodeIdInvalid {
		inodeId = c.qfs.newInodeId()
	}

	return inodeId
}

// Returns the inodeId used for the child
func (cmap *ChildMap) loadChild(c *ctx, entry quantumfs.DirectoryRecord,
	inodeId InodeId) InodeId {

	defer c.FuncIn("ChildMap::loadChild", "%s %s", entry.Filename(),
		entry.ID().String())

	entry = convertRecord(cmap.wsr, entry)
	inodeId = cmap.loadInodeId(c, entry, inodeId)

	if entry == nil {
		panic(fmt.Sprintf("Nil DirectoryEntryIf set attempt: %d", inodeId))
	}

	cmap.children[entry.Filename()] = inodeId
	// child is not dirty by default

	cmap.childrenRecords.setRecord(entry)

	return inodeId
}

func (cmap *ChildMap) count() uint64 {
	return uint64(len(cmap.children))
}

func (cmap *ChildMap) deleteChild(c *ctx,
	name string) (needsReparent quantumfs.DirectoryRecord) {

	defer c.FuncIn("ChildMap::deleteChild", "name %s", name).out()

	inodeId, exists := cmap.children[name]
	if !exists {
		c.vlog("name does not exist")
		return nil
	}

	record := cmap.recordByName(c, name)
	if record == nil {
		c.vlog("record does not exist")
		return nil
	}

	// This may be a hardlink that is due to be converted.
	if hardlink, isHardlink := record.(*Hardlink); isHardlink {
		var newRecord quantumfs.DirectoryRecord
		newRecord, inodeId = cmap.wsr.removeHardlink(c,
			hardlink.linkId)

		// Wsr says we're about to orphan the last hardlink copy
		if newRecord != nil || inodeId != quantumfs.InodeIdInvalid {
			newRecord.SetFilename(hardlink.Filename())
			record = newRecord
			cmap.loadChild(c, newRecord, inodeId)
		}
	}
	delete(cmap.children, name)
	result := cmap.recordByName(c, name)
	cmap.childrenRecords.delRecord(name)

	if link, isHardlink := record.(*Hardlink); isHardlink {
		if cmap.wsr.hardlinkDec(link.linkId) {
			// If the refcount was greater than one we shouldn't
			// reparent.
			c.vlog("Hardlink refernced elsewhere")
			return nil
		}
	}
	return result
}

func (cmap *ChildMap) renameChild(c *ctx, oldName string,
	newName string) (oldInodeRemoved InodeId) {

	defer c.FuncIn("ChildMap::renameChild", "oldName %s newName %s", oldName,
		newName).out()

	if oldName == newName {
		c.vlog("Names are identical")
		return quantumfs.InodeIdInvalid
	}

	inodeId, exists := cmap.children[oldName]
	if !exists {
		c.vlog("oldName doesn't exist")
		return quantumfs.InodeIdInvalid
	}

	record := cmap.recordByName(c, oldName)
	if record == nil {
		c.vlog("oldName record doesn't exist")
		panic("inode set without record")
	}

	// record whether we need to cleanup a file we're overwriting
	cleanupInodeId, needCleanup := cmap.children[newName]
	if needCleanup {
		// we have to cleanup before we move, to allow the case where we
		// rename a hardlink to an existing one with the same inode
		cmap.childrenRecords.delRecord(newName)
		delete(cmap.children, newName)
	}

	delete(cmap.children, oldName)
	cmap.childrenRecords.delRecord(oldName)

	cmap.children[newName] = inodeId
	record.SetFilename(newName)
	cmap.childrenRecords.setRecord(record)

	if needCleanup {
		c.vlog("cleanupInodeId %d", cleanupInodeId)
		return cleanupInodeId
	}

	return quantumfs.InodeIdInvalid
}

func (cmap *ChildMap) inodeNum(name string) InodeId {
	if inodeId, exists := cmap.children[name]; exists {
		return inodeId
	}

	return quantumfs.InodeIdInvalid
}

func (cmap *ChildMap) directInodes(c *ctx) []InodeId {
	rtn := make([]InodeId, 0)

	cmap.childrenRecords.iterateOverRecords(c,
		func (record quantumfs.DirectoryRecord) bool {

		if _, isHardlink := record.(*Hardlink); !isHardlink {
			rtn = append(rtn, cmap.children[record.Filename()])
		}
		return false
	})

	return rtn
}

func (cmap *ChildMap) recordCopies(c *ctx) []quantumfs.DirectoryRecord {
	rtn := make([]quantumfs.DirectoryRecord, 0)

	cmap.childrenRecords.iterateOverRecords(c,
		func (record quantumfs.DirectoryRecord) bool {

		rtn = append(rtn, record)
		return false
	})

	return rtn
}

func (cmap *ChildMap) recordCopy(c *ctx,
	inodeId InodeId) quantumfs.DirectoryRecord {

	// Just return the first matching inode id entry
	var rtn quantumfs.DirectoryRecord

	cmap.childrenRecords.iterateOverRecords(c,
		func (record quantumfs.DirectoryRecord) bool {

		inodeNum, exists := cmap.children[record.Filename()]
		if exists && inodeNum == inodeId {
			rtn = record
			return true
		}

		return false
	})

	return rtn
}

func (cmap *ChildMap) makeHardlink(c *ctx,
	childId InodeId) (copy quantumfs.DirectoryRecord, err fuse.Status) {

	defer c.FuncIn("ChildMap::makeHardlink", "inode %d", childId).out()

	child := cmap.recordCopy(c, childId)
	if child == nil {
		c.elog("No child record for inode id %d in childmap", childId)
		return nil, fuse.ENOENT
	}

	// If it's already a hardlink, great no more work is needed
	if link, isLink := child.(*Hardlink); isLink {
		c.vlog("Already a hardlink")

		recordCopy := *link

		// Ensure we update the ref count for this hardlink
		cmap.wsr.hardlinkInc(link.linkId)

		return &recordCopy, fuse.OK
	}

	// record must be a file type to be hardlinked
	if child.Type() != quantumfs.ObjectTypeSmallFile &&
		child.Type() != quantumfs.ObjectTypeMediumFile &&
		child.Type() != quantumfs.ObjectTypeLargeFile &&
		child.Type() != quantumfs.ObjectTypeVeryLargeFile &&
		child.Type() != quantumfs.ObjectTypeSymlink &&
		child.Type() != quantumfs.ObjectTypeSpecial {

		c.dlog("Cannot hardlink %s - not a file", child.Filename())
		return nil, fuse.EINVAL
	}

	// It needs to become a hardlink now. Hand it off to wsr
	c.vlog("Converting into a hardlink")
	newLink := cmap.wsr.newHardlink(c, childId, child)

	cmap.childrenRecords.setRecord(newLink)
	linkCopy := *newLink
	return &linkCopy, fuse.OK
}

func (cmap *ChildMap) publish(c *ctx) quantumfs.ObjectKey {
	return cmap.childrenRecords.publish(c)
}

type thinChildren struct {
	wsr	*WorkspaceRoot
	base	quantumfs.ObjectKey
	changes	map[string]quantumfs.DirectoryRecord
}

func newThinChildren (key quantumfs.ObjectKey, wsr_ *WorkspaceRoot) thinChildren {
	return thinChildren {
		wsr:		wsr_,
		base:		key,
		changes:	make(map[string]quantumfs.DirectoryRecord),
	}
}

func (th *thinChildren) iterateOverRecords(c *ctx,
	fxn func (quantumfs.DirectoryRecord) bool) {

	existingEntries := make(map[string]bool, 0)

	key := th.base
	for {
		buffer := c.dataStore.Get(&c.Ctx, key)
		if buffer == nil {
			panic("No baseLayer object")
		}

		baseLayer := buffer.AsDirectoryEntry()

		for i := 0; i < baseLayer.NumEntries(); i++ {
			entry := quantumfs.DirectoryRecord(baseLayer.Entry(i))

			// ensure we overwrite changes from the base
			record, exists := th.changes[entry.Filename()]
			if exists {
				// if the record is nil, that means it was deleted
				if record != nil {
					escape := fxn(record)
					if escape {
						return
					}
				}
			} else {
				entry = convertRecord(th.wsr, baseLayer.Entry(i))

				// cache the vanilla entry to improve performance
				th.changes[entry.Filename()] = entry

				escape := fxn(entry)
				if escape {
					return
				}
			}

			existingEntries[entry.Filename()] = true
		}

		if baseLayer.HasNext() {
			key = baseLayer.Next()
		} else {
			break
		}
	}

	// don't forget added entries
	for name, record := range th.changes {
		if record == nil {
			continue
		}

		if _, exists := existingEntries[name]; !exists {
			escape := fxn(record)
			if escape {
				return
			}
		}
	}
}

func (th *thinChildren) setRecord(record quantumfs.DirectoryRecord) {
	th.changes[record.Filename()] = record
}

func (th *thinChildren) delRecord(name string) {
	th.changes[name] = nil
}

func (th *thinChildren) publish(c *ctx) quantumfs.ObjectKey {

	defer c.funcIn("thinChildren::publish").out()

	// Compile the internal records into a series of blocks which can be placed
	// in the datastore.
	newBaseLayerId := quantumfs.EmptyDirKey

	// childIdx indexes into dir.childrenRecords, entryIdx indexes into the
	// metadata block
	baseLayer := quantumfs.NewDirectoryEntry()
	entryIdx := 0
	th.iterateOverRecords(c, func (record quantumfs.DirectoryRecord) bool {
		if entryIdx == quantumfs.MaxDirectoryRecords() {
			// This block is full, upload and create a new one
			c.vlog("Block full with %d entries", entryIdx)
			baseLayer.SetNumEntries(entryIdx)
			newBaseLayerId = publishDirectoryEntry(c, baseLayer,
				newBaseLayerId)
			baseLayer = quantumfs.NewDirectoryEntry()
			entryIdx = 0
		}

		recordCopy := record.Record()
		baseLayer.SetEntry(entryIdx, &recordCopy)

		entryIdx++
		return false
	})

	baseLayer.SetNumEntries(entryIdx)
	newBaseLayerId = publishDirectoryEntry(c, baseLayer, newBaseLayerId)

	// update our state
	th.base = newBaseLayerId
	th.changes = make(map[string]quantumfs.DirectoryRecord)

	return newBaseLayerId
}

func publishDirectoryEntry(c *ctx, layer *quantumfs.DirectoryEntry,
	nextKey quantumfs.ObjectKey) quantumfs.ObjectKey {

	defer c.funcIn("publishDirectoryEntry").out()

	layer.SetNext(nextKey)
	bytes := layer.Bytes()

	buf := newBuffer(c, bytes, quantumfs.KeyTypeMetadata)
	newKey, err := buf.Key(&c.Ctx)
	if err != nil {
		panic("Failed to upload new baseLayer object")
	}

	return newKey
}

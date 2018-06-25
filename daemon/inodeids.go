// Copyright (c) 2018 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

import (
	"container/list"
	"time"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/utils"
)

type reusableId struct {
	id     InodeId
	usable time.Time
}

func newInodeIds(delay time.Duration, gcPeriod time.Duration) *inodeIds {
	return &inodeIds{
		highMark:      quantumfs.InodeIdReservedEnd + 1,
		gcPeriod:      gcPeriod,
		reusableMap:   make(map[InodeId]struct{}),
		reusableDelay: delay,
	}
}

type inodeIds struct {
	highMark uint64
	// The last time of garbage collection or a change in highMark
	lastEvent time.Time
	gcPeriod  time.Duration

	reusableIds list.List
	reusableMap map[InodeId]struct{}
	lock        utils.DeferableMutex

	// configurations
	reusableDelay time.Duration
}

func (ids *inodeIds) newInodeId(c *ctx) InodeId {
	defer ids.lock.Lock().Unlock()

	ids.garbageCollect_(c)

	for {
		nextIdElem := ids.reusableIds.Front()
		if nextIdElem == nil {
			// no ids left to reuse
			break
		}

		nextId := nextIdElem.Value.(reusableId)
		if nextId.usable.After(time.Now()) {
			// the next tuple is too new to use
			break
		}

		// now that we know this tuple isn't under delay, we will either
		// return it or garbage collect it
		ids.remove_(nextId.id, nextIdElem)

		if uint64(nextId.id) < ids.highMark {
			// this id is useable
			return nextId.id
		}

		// discard to garbage collect and try the next element
	}

	// we didn't find an id to reuse, so return a fresh one
	return ids.allocateFreshId_()
}

func (ids *inodeIds) releaseInodeId(c *ctx, id InodeId) {
	defer ids.lock.Lock().Unlock()

	ids.garbageCollect_(c)

	if uint64(id) >= ids.highMark {
		// garbage collect this id
		return
	}

	ids.push_(id)
}

const inodeIdsGb = "Garbage collected highmark"

func (ids *inodeIds) garbageCollect_(c *ctx) {
	if time.Since(ids.lastEvent) > ids.gcPeriod {
		ids.lastEvent = ids.lastEvent.Add(ids.gcPeriod)
		newHighMark := uint64(0.9 * float64(ids.highMark))
		c.vlog(inodeIdsGb, ids.highMark, newHighMark)
		ids.highMark = newHighMark
		if ids.highMark < quantumfs.InodeIdReservedEnd+1 {
			ids.highMark = quantumfs.InodeIdReservedEnd + 1
		}
	}
}

// ids.lock must be locked
func (ids *inodeIds) allocateFreshId_() InodeId {
	ids.lastEvent = time.Now()

	for {
		nextId := InodeId(ids.highMark)
		ids.highMark++

		_, exists := ids.reusableMap[nextId]
		if !exists {
			// this id isn't on a delay, so it's safe to use
			return nextId
		}
	}
}

// ids.lock must be locked
func (ids *inodeIds) push_(id InodeId) {
	ids.reusableIds.PushBack(reusableId{
		id:     id,
		usable: time.Now().Add(ids.reusableDelay),
	})
	ids.reusableMap[id] = struct{}{}
}

// ids.lock must be locked. id an idElem must match
func (ids *inodeIds) remove_(id InodeId, idElem *list.Element) {
	ids.reusableIds.Remove(idElem)
	delete(ids.reusableMap, id)
}

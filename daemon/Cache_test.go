// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package daemon

// Test the internal datastore cache

import (
	"fmt"
	"sync"
	"testing"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/backends/processlocal"
	"github.com/aristanetworks/quantumfs/qlog"
	"github.com/aristanetworks/quantumfs/utils"
)

type testDataStore struct {
	datastore  quantumfs.DataStore
	shouldRead bool
	test       *testHelper

	countLock utils.DeferableMutex
	getCount  map[string]int
	setCount  map[string]int

	// Allows you to effectively delete blocks
	holeLock utils.DeferableMutex
	holes    map[string]struct{}
}

func newTestDataStore(test *testHelper) *testDataStore {
	return &testDataStore{
		datastore:  processlocal.NewDataStore(""),
		shouldRead: true,
		test:       test,
		getCount:   make(map[string]int),
		setCount:   make(map[string]int),
		holes:      make(map[string]struct{}),
	}
}

func (store *testDataStore) Get(c *quantumfs.Ctx, key quantumfs.ObjectKey,
	buf quantumfs.Buffer) error {

	store.test.Assert(store.shouldRead, "Received unexpected Get for %s",
		key.String())

	func() {
		defer store.countLock.Lock().Unlock()
		store.getCount[key.String()]++
	}()

	err := func() error {
		defer store.holeLock.Lock().Unlock()
		if _, exists := store.holes[key.String()]; exists {
			return fmt.Errorf("Key does not exist")
		}

		return nil
	}()
	if err != nil {
		return err
	}

	return store.datastore.Get(c, key, buf)
}

func (store *testDataStore) Set(c *quantumfs.Ctx, key quantumfs.ObjectKey,
	buf quantumfs.Buffer) error {

	func() {
		defer store.countLock.Lock().Unlock()
		store.setCount[key.String()]++
	}()

	return store.datastore.Set(c, key, buf)
}

func (store *testDataStore) Freshen(c *quantumfs.Ctx,
	key quantumfs.ObjectKey) error {

	func() {
		defer store.countLock.Lock().Unlock()
		store.setCount[key.String()]++
	}()

	return store.datastore.Freshen(c, key)
}

func createBuffer(c *quantumfs.Ctx, test *testHelper, backingStore *testDataStore,
	datastore *dataStore, keys map[int]quantumfs.ObjectKey, indx, size int) {
	bytes := make([]byte, size*quantumfs.ObjectKeyLength)
	bytes[1] = byte(indx % 256)
	bytes[2] = byte(indx / 256)
	key := quantumfs.NewObjectKeyFromBytes(
		bytes[:quantumfs.ObjectKeyLength])
	keys[indx] = key
	buff := &buffer{
		data:      bytes,
		dirty:     false,
		keyType:   quantumfs.KeyTypeData,
		key:       key,
		dataStore: datastore,
	}
	err := backingStore.Set(c, key, buff)
	test.Assert(err == nil, "Error priming datastore: %v", err)

}

func fillDatastore(c *quantumfs.Ctx, test *testHelper, backingStore *testDataStore,
	datastore *dataStore, entryNum int, keys map[int]quantumfs.ObjectKey) {

	for i := 1; i < entryNum; i++ {
		_, exists := keys[i]
		// Only fill the keys which haven't been filled
		if !exists {
			createBuffer(c, test, backingStore, datastore, keys, i, 1)
		}
	}
}

func createDatastore(test *testHelper, entryNum, cacheSize int) (c *quantumfs.Ctx,
	backingStore *testDataStore, datastore *dataStore,
	keys map[int]quantumfs.ObjectKey) {

	backingStore = newTestDataStore(test)
	datastore = newDataStore(backingStore, cacheSize)

	keys = make(map[int]quantumfs.ObjectKey, entryNum)

	ctx := ctx{
		Ctx: quantumfs.Ctx{
			Qlog:      test.Logger,
			RequestId: qlog.TestReqId,
		},
	}
	c = &ctx.Ctx

	return c, backingStore, datastore, keys
}

func TestCacheLru(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		entryNum := 512
		cacheSize := (entryNum / 2) * quantumfs.ObjectKeyLength
		c, backingStore, datastore, keys := createDatastore(test,
			entryNum, cacheSize)
		defer datastore.shutdown()
		fillDatastore(c, test, backingStore, datastore, entryNum, keys)

		// Prime the LRU by reading every entry in reverse order. At the end
		// we should have the first (entryNum/2) entries in the cache.
		test.Log("Priming LRU")
		for i := entryNum - 1; i > 0; i-- {
			buf := datastore.Get(c, keys[i]).(*buffer)
			test.Assert(buf != nil, "Failed retrieving block %d", i)
		}
		test.Log("Verifying cache")

		func() {
			defer datastore.cache.lock.Lock().Unlock()
			test.Assert(datastore.cache.size == cacheSize,
				"Incorrect Cache size %d != %d",
				datastore.cache.size, cacheSize)
			lruNum := cacheSize / quantumfs.ObjectKeyLength
			for _, v := range datastore.cache.entryMap {
				buf := v.buf.(*buffer)
				i := int(buf.data[1]) + int(buf.data[2])*256
				test.Assert(i <= lruNum,
					"Unexpected block in cache %d", i)
			}
			test.Log("Verifying LRU")
			freeSpace := cacheSize % quantumfs.ObjectKeyLength
			test.Assert(datastore.cache.lru.Len() == lruNum,
				"Incorrect Lru size %d != %d ", lruNum,
				datastore.cache.lru.Len())
			test.Assert(datastore.cache.freeSpace == freeSpace,
				"Incorrect Free space %d != %d", freeSpace,
				datastore.cache.freeSpace)
			num := 1
			for e := datastore.cache.lru.Back(); e != nil; e = e.Prev() {
				buf := e.Value.(*cacheEntry).buf.(*buffer)
				i := int(buf.data[1]) + int(buf.data[2])*256
				test.Assert(i <= lruNum,
					"Unexpected block in lru %d", i)
				test.Assert(i == num,
					"Out of order block %d not %d", i, num)
				num++
			}
		}()

		// Cause a block to be refreshed to the beginning
		buf := datastore.Get(c, keys[256]).(*buffer)
		test.Assert(buf != nil, "Block not found")

		defer datastore.cache.lock.Lock().Unlock()
		data := datastore.cache.lru.Back().Value.(*cacheEntry).buf.(*buffer)
		i := int(data.data[1]) + int(data.data[2])*256
		test.Assert(i == 256, "Incorrect most recent block %d != 256", i)

		data = datastore.cache.lru.Front().Value.(*cacheEntry).buf.(*buffer)
		i = int(data.data[1]) + int(data.data[2])*256
		test.Assert(i == 255, "Wrong least recent block %d != 255", i)
	})
}

func TestCacheLruDiffSize(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		entryNum := 280
		cacheSize := 12 + 100*quantumfs.ObjectKeyLength
		c, backingStore, datastore, keys := createDatastore(test,
			entryNum, cacheSize)
		defer datastore.shutdown()

		// Add a content with size greater than datastore.size, and set
		// different sizes of several keys in advance.
		createBuffer(c, test, backingStore, datastore, keys, 1, 2)
		createBuffer(c, test, backingStore, datastore, keys, 4, 15)
		createBuffer(c, test, backingStore, datastore, keys, 13, 6)
		createBuffer(c, test, backingStore, datastore, keys, 71, 30)
		createBuffer(c, test, backingStore, datastore, keys, 257, 120)
		createBuffer(c, test, backingStore, datastore, keys, 298, 72)
		fillDatastore(c, test, backingStore, datastore, entryNum, keys)

		test.Log("Priming LRU")
		for i := entryNum - 1; i > 0; i-- {
			buf := datastore.Get(c, keys[i]).(*buffer)
			test.Assert(buf != nil, "Failed retrieving block %d", i)
		}
		// Update keys[257] whose size is greater than cacheSize, but it
		// should not have an impact on the final result of lru list because
		// it is too large
		buf := datastore.Get(c, keys[257]).(*buffer)
		test.Assert(buf != nil, "Failed retrieving block 257")

		test.Log("Verifying cache")

		func() {
			defer datastore.cache.lock.Lock().Unlock()

			test.Assert(datastore.cache.size == cacheSize,
				"Incorrect cache size %d != %d",
				datastore.cache.size, cacheSize)
			// Since keys[71] is too large, cache will contain the first
			// 70 entries, and we can calculate free space accordingly
			lruNum := 70
			for _, v := range datastore.cache.entryMap {
				buf := v.buf.(*buffer)
				i := int(buf.data[1]) + int(buf.data[2])*256
				test.Assert(i <= lruNum,
					"Unexpected block in cache %d", i)
			}
			test.Log("Verifying LRU")
			test.Assert(datastore.cache.lru.Len() == lruNum,
				"Incorrect Lru size %d != %d ", lruNum,
				datastore.cache.lru.Len())
			freeSpace := 10*quantumfs.ObjectKeyLength + 12
			test.Assert(datastore.cache.freeSpace == freeSpace,
				"Incorrect Free space %d != %d", freeSpace,
				datastore.cache.freeSpace)
			num := 1
			for e := datastore.cache.lru.Back(); e != nil; e = e.Prev() {
				buf := e.Value.(*cacheEntry).buf.(*buffer)
				i := int(buf.data[1]) + int(buf.data[2])*256
				test.Assert(i <= lruNum,
					"Unexpected block in lru %d", i)
				test.Assert(i == num, "Out of order block %d not %d",
					i, num)
				num++
			}
		}()

		// Cause a block to be refreshed to the beginning

		// The size of keys[298] is 72 units of quantumfs.ObjectKeyLength, so
		// it will take off the space up to keys[14] and part of keys[13].
		// Due to the mechanism of cache, all keys from keys[70] downto
		// keys[13] should be evicted.
		buf = datastore.Get(c, keys[298]).(*buffer)
		test.Assert(buf != nil, "Block not found")

		defer datastore.cache.lock.Lock().Unlock()
		data := datastore.cache.lru.Back().Value.(*cacheEntry).buf.(*buffer)
		i := int(data.data[1]) + int(data.data[2])*256
		test.Assert(i == 298, "Incorrect most recent block %d != 298", i)

		// The front element is supposed to be keys[13], but its size is too
		// large, so it has to be removed from the cache
		data = datastore.cache.lru.Front().Value.(*cacheEntry).buf.(*buffer)
		i = int(data.data[1]) + int(data.data[2])*256
		test.Assert(i == 12, "Wrong least recent block %d != 12", i)
	})
}

func TestCacheCaching(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		entryNum := 256
		c, backingStore, datastore, keys := createDatastore(test,
			entryNum, 100*quantumfs.ObjectKeyLength)
		defer datastore.shutdown()

		// Add a content with size greater than datastore.size, and
		// double the size of keys[1] in advance.
		createBuffer(c, test, backingStore, datastore, keys, 257, 101)
		createBuffer(c, test, backingStore, datastore, keys, 1, 2)
		fillDatastore(c, test, backingStore, datastore, entryNum, keys)

		// Prime the cache
		for i := 1; i <= 100; i++ {
			buf := datastore.Get(c, keys[i]).(*buffer)
			test.Assert(buf != nil, "Failed to get block %d", i)
		}
		buf := datastore.Get(c, keys[257]).(*buffer)
		test.Assert(buf != nil, "Failed to get block 257")
		test.Assert(buf.Size() == 101*quantumfs.ObjectKeyLength,
			"Incorrect length of block 257: %d != %d", buf.Size(),
			101*quantumfs.ObjectKeyLength)

		func() {
			defer datastore.cache.lock.Lock().Unlock()

			// Since the size of keys[1] is doubled, so it is removed
			// from the cache, so the usage of space is
			// 99*quantumfs.ObjectKeyLength
			test.Assert(datastore.cache.freeSpace == 0+
				quantumfs.ObjectKeyLength,
				"Failed memory management: %d != %d",
				datastore.cache.freeSpace, quantumfs.ObjectKeyLength)

			backingStore.shouldRead = false

			// Because of the size constraint, the least recent used
			// entry keys[1] should be deleted from cache
			_, exists := datastore.cache.entryMap[keys[1].String()]
			test.Assert(!exists, "Failed to forget block 1")
			// The content is oversized, so it should be stored in cache
			_, exists = datastore.cache.entryMap[keys[257].String()]
			test.Assert(!exists, "Failed to forget block 257")
		}()

		// Reading again should come entirely from the cache. If not
		// testDataStore will assert.
		for i := 2; i <= 100; i++ {
			buf := datastore.Get(c, keys[i]).(*buffer)
			test.Assert(buf != nil, "Failed to get block %d", i)
		}
	})
}

func TestCacheCombining(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		entryNum := 256
		c, backingStore, datastore, keys := createDatastore(test,
			entryNum, 100*quantumfs.ObjectKeyLength)
		defer datastore.shutdown()
		fillDatastore(c, test, backingStore, datastore, entryNum, keys)

		checkKey := keys[1]

		// Run parallel gets and check to ensure that we only Get once
		getsBefore := func() int {
			defer backingStore.countLock.Lock().Unlock()
			return backingStore.getCount[checkKey.String()]
		}()

		// Pre-lock the countLock to block the next Gets so we know that
		// they happen in parallel
		backingStore.countLock.Lock()

		parallelReqs := 10
		var wg sync.WaitGroup
		for j := 0; j < parallelReqs; j++ {
			wg.Add(1)
			go func() {
				datastore.Get(c, checkKey)
				wg.Done()
			}()
		}

		test.WaitFor("All datastore Gets to queue up", func() bool {
			defer datastore.cache.lock.Lock().Unlock()
			keyEntry, exists := datastore.cache.entryMap[""+
				checkKey.String()]
			if !exists {
				return false
			}
			return len(keyEntry.waiting) == parallelReqs
		})

		// now unlock the lock to let anything through
		backingStore.countLock.Unlock()

		// wait for every call to finish
		wg.Wait()

		getsAfter := func() int {
			defer backingStore.countLock.Lock().Unlock()
			return backingStore.getCount[checkKey.String()]
		}()

		// Even though we bunched Gets for the same block in parallel,
		// they should have been combined and only one Get triggered
		test.Assert(getsAfter == getsBefore+1,
			"Gets not properly combined: %d vs %d", getsBefore,
			getsAfter)
	})
}

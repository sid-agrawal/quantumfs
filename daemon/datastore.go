// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

import "crypto/sha1"

import "github.com/aristanetworks/quantumfs"
import "github.com/aristanetworks/quantumfs/encoding"
import "github.com/aristanetworks/quantumfs/qlog"
import capn "github.com/glycerine/go-capnproto"

func newDataStore(durableStore quantumfs.DataStore) *dataStore {
	return &dataStore{
		durableStore: durableStore,
	}
}

type dataStore struct {
	durableStore quantumfs.DataStore
}

func (store *dataStore) Get(c *quantumfs.Ctx,
	key quantumfs.ObjectKey) quantumfs.Buffer {

	if key.Type() == quantumfs.KeyTypeEmbedded {
		panic("Attempted to fetch embedded key")
	}

	var buf buffer
	initBuffer(&buf, store, key)

	err := quantumfs.ConstantStore.Get(c, key, &buf)
	if err == nil {
		return &buf
	}

	err = store.durableStore.Get(c, key, &buf)
	if err == nil {
		return &buf
	}
	c.Elog(qlog.LogDaemon, "Couldn't get from any store: %v. Key %s", err, key)

	return nil
}

func (store *dataStore) Set(c *quantumfs.Ctx, buffer quantumfs.Buffer) error {
	key, err := buffer.Key(c)
	if err != nil {
		return err
	}

	if key.Type() == quantumfs.KeyTypeEmbedded {
		panic("Attempted to set embedded key")
	}
	return store.durableStore.Set(c, key, buffer)
}

// buffer is the central data-handling type of quantumfsd
func newBuffer(c *ctx, in []byte, keyType quantumfs.KeyType) quantumfs.Buffer {
	return &buffer{
		data:      in,
		dirty:     true,
		keyType:   keyType,
		dataStore: c.dataStore,
	}
}

func initBuffer(buf *buffer, dataStore *dataStore, key quantumfs.ObjectKey) {
	buf.dirty = false
	buf.dataStore = dataStore
	buf.keyType = key.Type()
	buf.key = key
}

type buffer struct {
	data      []byte
	dirty     bool
	keyType   quantumfs.KeyType
	key       quantumfs.ObjectKey
	dataStore *dataStore
}

func (buf *buffer) Write(c *quantumfs.Ctx, in []byte, offset uint32) uint32 {
	// Sanity check offset and length
	maxWriteLen := quantumfs.MaxBlockSize - int(offset)
	if maxWriteLen <= 0 {
		return 0
	}

	if len(in) > maxWriteLen {
		in = in[:maxWriteLen]
	}

	// Ensure that our data ends where we need it to. This allows us to write
	// past the end of a block, but not past the block's max capacity
	deltaLen := int(offset) - len(buf.data)
	if deltaLen > 0 {
		buf.data = append(buf.data, make([]byte, deltaLen)...)
	}

	var finalBuffer []byte
	// append our write data to the first split of the existing data
	finalBuffer = append(buf.data[:offset], in...)

	// record how much was actually appended (in case len(in) < size)
	copied := uint32(len(finalBuffer)) - uint32(offset)

	// then add on the rest of the existing data afterwards, excluding the amount
	// that we just wrote (to overwrite instead of insert)
	remainingStart := offset + copied
	if int(remainingStart) < len(buf.data) {
		finalBuffer = append(finalBuffer, buf.data[remainingStart:]...)
	}

	c.Vlog(qlog.LogDaemon, "Marking buffer dirty")
	buf.dirty = true
	buf.data = finalBuffer

	return copied
}

func (buf *buffer) Read(out []byte, offset uint32) int {
	return copy(out, buf.data[offset:])
}

func (buf *buffer) Get() []byte {
	return buf.data
}

func (buf *buffer) Set(data []byte, keyType quantumfs.KeyType) {
	buf.data = data
	buf.keyType = keyType
	buf.dirty = true
}

func (buf *buffer) ContentHash() [quantumfs.ObjectKeyLength - 1]byte {
	return sha1.Sum(buf.data)
}

func (buf *buffer) Key(c *quantumfs.Ctx) (quantumfs.ObjectKey, error) {
	if !buf.dirty {
		c.Vlog(qlog.LogDaemon, "Buffer not dirty")
		return buf.key, nil
	}

	buf.key = quantumfs.NewObjectKey(buf.keyType, buf.ContentHash())
	buf.dirty = false
	c.Vlog(qlog.LogDaemon, "New buffer key %x", buf.key.String())
	err := buf.dataStore.Set(c, buf)
	return buf.key, err
}

func (buf *buffer) SetSize(size int) {
	if size > quantumfs.MaxBlockSize {
		panic("New block size greater than maximum")
	}

	if len(buf.data) > size {
		buf.data = buf.data[:size]
		return
	}

	for len(buf.data) < size {
		extraBytes := make([]byte, size-len(buf.data))
		buf.data = append(buf.data, extraBytes...)
	}

	buf.dirty = true
}

func (buf *buffer) Size() int {
	return len(buf.data)
}

func (buf *buffer) AsDirectoryEntry() quantumfs.DirectoryEntry {
	segment := capn.NewBuffer(buf.data)
	return quantumfs.OverlayDirectoryEntry(
		encoding.ReadRootDirectoryEntry(segment))

}

func (buf *buffer) AsWorkspaceRoot() quantumfs.WorkspaceRoot {
	segment := capn.NewBuffer(buf.data)
	return quantumfs.OverlayWorkspaceRoot(
		encoding.ReadRootWorkspaceRoot(segment))

}

func (buf *buffer) AsMultiBlockFile() quantumfs.MultiBlockFile {
	segment := capn.NewBuffer(buf.data)
	return quantumfs.OverlayMultiBlockFile(
		encoding.ReadRootMultiBlockFile(segment))

}

func (buf *buffer) AsVeryLargeFile() quantumfs.VeryLargeFile {
	segment := capn.NewBuffer(buf.data)
	return quantumfs.OverlayVeryLargeFile(
		encoding.ReadRootVeryLargeFile(segment))

}

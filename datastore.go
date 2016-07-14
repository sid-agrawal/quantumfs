// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// The datastore interface
package quantumfs

import "crypto/sha1"
import "encoding/binary"
import "fmt"
import "time"

import "github.com/aristanetworks/quantumfs/encoding"
import "github.com/aristanetworks/quantumfs/qlog"
import capn "github.com/glycerine/go-capnproto"

// Maximum size of a block which can be stored in a datastore
const MaxBlockSize = int(encoding.MaxBlockSize)

// Maximum number of blocks for each file type
const MaxBlocksMediumFile = int(encoding.MaxBlocksMediumFile)

// TODO: Increase these to 48000 when we choose a more efficient encoding than json
const MaxBlocksLargeFile = int(encoding.MaxBlocksLargeFile)

// TODO: Increase this to 48000 when we switch away from json
const MaxPartsVeryLargeFile = int(encoding.MaxPartsVeryLargeFile)

const MaxDirectoryRecords = int(encoding.MaxDirectoryRecords)

// Maximum length of a filename
const MaxFilenameLength = int(encoding.MaxFilenameLength)

// Special reserved namespace/workspace names
const (
	ApiPath           = "api" // File used for the qfs api
	NullNamespaceName = "_null"
	NullWorkspaceName = "null"
)

// Special reserved inode numbers
const (
	_                  = iota // Invalid
	InodeIdRoot        = iota // Same as fuse.FUSE_ROOT_ID
	InodeIdApi         = iota // /api file
	InodeId_null       = iota // /_null namespace
	InodeId_nullNull   = iota // /_null/null workspace
	InodeIdReservedEnd = iota // End of the reserved range
)

// Object key types, possibly used for datastore routing
const (
	KeyTypeConstant  = iota // A statically known object, such as the empty block
	KeyTypeOther     = iota // A nonspecific type
	KeyTypeMetadata  = iota // Metadata block ie a directory or file descrptor
	KeyTypeBuildable = iota // A block generated by a build
	KeyTypeData      = iota // A block generated by a user
	KeyTypeVCS       = iota // A block which is backed by a VCS
)

// String names for KeyTypes
func KeyTypeToString(keyType KeyType) string {
	switch keyType {
	default:
		return "Unknown"
	case KeyTypeConstant:
		return "Constant"
	case KeyTypeOther:
		return "Other"
	case KeyTypeMetadata:
		return "Metadata"
	case KeyTypeBuildable:
		return "Buildable"
	case KeyTypeData:
		return "Data"
	case KeyTypeVCS:
		return "VCS"
	}
}

// One of the KeyType* values above
type KeyType uint8

// The size of the object ID is determined by a number of bytes sufficient to contain
// the identification hashes used by all the backing stores (most notably the VCS
// such as git or Mercurial) and additional space to be used for datastore routing.
//
// In this case we use a 20 byte hash sufficient to store sha1 values and one
// additional byte used for routing.
const ObjectKeyLength = 1 + sha1.Size

type ObjectKey struct {
	key encoding.ObjectKey
}

func NewObjectKey(type_ KeyType, hash [ObjectKeyLength - 1]byte) ObjectKey {
	segment := capn.NewBuffer(nil)
	key := ObjectKey{
		key: encoding.NewRootObjectKey(segment),
	}

	key.key.SetKeyType(byte(type_))
	key.key.SetPart2(binary.LittleEndian.Uint64(hash[:8]))
	key.key.SetPart3(binary.LittleEndian.Uint64(hash[8:16]))
	key.key.SetPart4(binary.LittleEndian.Uint32(hash[16:]))
	return key
}

func overlayObjectKey(k encoding.ObjectKey) ObjectKey {
	key := ObjectKey{
		key: k,
	}
	return key
}

// Extract the type of the object. Returns a KeyType
func (key ObjectKey) Type() KeyType {
	return KeyType(key.key.KeyType())
}

func (key ObjectKey) String() string {
	hex := fmt.Sprintf("(%s: %016x%016x%08x)", KeyTypeToString(key.Type()),
		key.key.Part2(), key.key.Part3(), key.key.Part4())

	return hex
}

func (key ObjectKey) Bytes() []byte {
	return key.key.Segment.Data
}

type DirectoryEntry struct {
	dir encoding.DirectoryEntry
}

func NewDirectoryEntry() *DirectoryEntry {
	segment := capn.NewBuffer(nil)

	dirEntry := DirectoryEntry{
		dir: encoding.NewRootDirectoryEntry(segment),
	}
	dirEntry.dir.SetNumEntries(0)

	recordList := encoding.NewDirectoryRecordList(segment, MaxDirectoryRecords)
	dirEntry.dir.SetEntries(recordList)

	return &dirEntry
}

func OverlayDirectoryEntry(edir encoding.DirectoryEntry) DirectoryEntry {
	dir := DirectoryEntry{
		dir: edir,
	}
	return dir
}

func (dir *DirectoryEntry) Bytes() []byte {
	return dir.dir.Segment.Data
}

func (dir *DirectoryEntry) NumEntries() int {
	return int(dir.dir.NumEntries())
}

func (dir *DirectoryEntry) SetNumEntries(n int) {
	dir.dir.SetNumEntries(uint32(n))
}

func (dir *DirectoryEntry) Entry(i int) DirectoryRecord {
	return overlayDirectoryRecord(dir.dir.Entries().At(i))
}

func (dir *DirectoryEntry) SetEntry(i int, record *DirectoryRecord) {
	dir.dir.Entries().Set(i, record.record)
}

func (dir *DirectoryEntry) Next() ObjectKey {
	return overlayObjectKey(dir.dir.Next())
}

func (dir *DirectoryEntry) SetNext(key ObjectKey) {
	dir.dir.SetNext(key.key)
}

// The various types the next referenced object could be
const (
	ObjectTypeBuildProduct      = iota
	ObjectTypeDirectoryEntry    = iota
	ObjectTypeExtendedAttribute = iota
	ObjectTypeHardlink          = iota
	ObjectTypeSymlink           = iota
	ObjectTypeVCSFile           = iota
	ObjectTypeWorkspaceRoot     = iota
	ObjectTypeSmallFile         = iota
	ObjectTypeMediumFile        = iota
	ObjectTypeLargeFile         = iota
	ObjectTypeVeryLargeFile     = iota
)

// One of the ObjectType* values
type ObjectType uint8

// Quantumfs doesn't keep precise ownership values. Instead files and directories may
// be owned by some special system accounts or the current user. The translation to
// UID is done at access time.
const (
	UIDRoot = iota
	UIDUser = iota // The currently accessing user
)

// Convert object UID to system UID.
//
// userId is the UID of the current user
func SystemUid(uid UID, userId uint32) uint32 {
	switch uid {
	case UIDRoot:
		return 0
	case UIDUser:
		return userId
	default:
		return 0
	}
}

// Convert system UID to object UID
//
// userId is the UID of the current user
func ObjectUid(c Ctx, uid uint32, userId uint32) UID {
	if uid == userId {
		return UIDUser
	}

	switch uid {
	case 0:
		return UIDRoot
	default:
		c.Elog(qlog.LogDatastore, "Unknown UID %d", uid)
		return UIDUser
	}
}

// One of the UID* values
type UID uint8

// Similar to the UIDs above, group ownership is divided into special classes.
const (
	GIDRoot = iota
	GIDUser = iota // The currently accessing user
)

// Convert object GID to system GID.
//
// userId is the GID of the current user
func SystemGid(gid GID, userId uint32) uint32 {
	switch gid {
	case GIDRoot:
		return 0
	case GIDUser:
		return userId
	default:
		return 0
	}
}

// Convert system GID to object GID
//
// userId is the GID of the current user
func ObjectGid(c Ctx, gid uint32, userId uint32) GID {
	if gid == userId {
		return GIDUser
	}

	switch gid {
	case 0:
		return GIDRoot
	default:
		c.Elog(qlog.LogDatastore, "Unknown GID", gid)
		return GIDUser
	}
}

// One of the GID* values
type GID uint8

// Quantumfs stores time in microseconds since the Unix epoch
type Time uint64

func (t Time) Seconds() uint64 {
	return uint64(t / 1000000)
}

func (t Time) Nanoseconds() uint32 {
	return uint32(t % 1000000)
}

func NewTime(instant time.Time) Time {
	t := instant.Unix() * 1000000
	t += int64(instant.Nanosecond() / 1000)

	return Time(t)
}

func NewTimeSeconds(seconds uint64, nanoseconds uint32) Time {
	t := seconds * 1000000
	t += uint64(nanoseconds / 1000)

	return Time(t)
}

var EmptyDirKey ObjectKey

func createEmptyDirectory() ObjectKey {
	emptyDir := NewDirectoryEntry()

	bytes := emptyDir.Bytes()

	hash := sha1.Sum(bytes)
	emptyDirKey := NewObjectKey(KeyTypeConstant, hash)
	constStore.store[emptyDirKey.String()] = bytes
	return emptyDirKey
}

var EmptyBlockKey ObjectKey

func createEmptyBlock() ObjectKey {
	var bytes []byte

	hash := sha1.Sum(bytes)
	emptyBlockKey := NewObjectKey(KeyTypeConstant, hash)
	constStore.store[emptyBlockKey.String()] = bytes
	return emptyBlockKey
}

func NewWorkspaceRoot() *WorkspaceRoot {
	segment := capn.NewBuffer(nil)
	wsr := WorkspaceRoot{
		wsr: encoding.NewRootWorkspaceRoot(segment),
	}

	return &wsr
}

type WorkspaceRoot struct {
	wsr encoding.WorkspaceRoot
}

func OverlayWorkspaceRoot(ewsr encoding.WorkspaceRoot) WorkspaceRoot {
	wsr := WorkspaceRoot{
		wsr: ewsr,
	}
	return wsr
}

func (wsr *WorkspaceRoot) Bytes() []byte {
	return wsr.wsr.Segment.Data
}

func (wsr *WorkspaceRoot) BaseLayer() ObjectKey {
	return overlayObjectKey(wsr.wsr.BaseLayer())
}

func (wsr *WorkspaceRoot) SetBaseLayer(key ObjectKey) {
	wsr.wsr.SetBaseLayer(key.key)
}

func (wsr *WorkspaceRoot) VcsLayer() ObjectKey {
	return overlayObjectKey(wsr.wsr.VcsLayer())
}

func (wsr *WorkspaceRoot) SetVcsLayer(key ObjectKey) {
	wsr.wsr.SetBaseLayer(key.key)
}

func (wsr *WorkspaceRoot) BuildLayer() ObjectKey {
	return overlayObjectKey(wsr.wsr.BuildLayer())
}

func (wsr *WorkspaceRoot) SetBuildLayer(key ObjectKey) {
	wsr.wsr.SetBuildLayer(key.key)
}

func (wsr *WorkspaceRoot) UserLayer() ObjectKey {
	return overlayObjectKey(wsr.wsr.UserLayer())
}

func (wsr *WorkspaceRoot) SetUserLayer(key ObjectKey) {
	wsr.wsr.SetUserLayer(key.key)
}

var EmptyWorkspaceKey ObjectKey

func createEmptyWorkspace(emptyDirKey ObjectKey) ObjectKey {
	emptyWorkspace := NewWorkspaceRoot()
	emptyWorkspace.SetBaseLayer(emptyDirKey)
	emptyWorkspace.SetVcsLayer(emptyDirKey)
	emptyWorkspace.SetBuildLayer(emptyDirKey)
	emptyWorkspace.SetUserLayer(emptyDirKey)

	bytes := emptyWorkspace.Bytes()

	hash := sha1.Sum(bytes)
	emptyWorkspaceKey := NewObjectKey(KeyTypeConstant, hash)
	constStore.store[emptyWorkspaceKey.String()] = bytes
	return emptyWorkspaceKey
}

func NewDirectoryRecord() *DirectoryRecord {
	segment := capn.NewBuffer(nil)
	record := DirectoryRecord{
		record: encoding.NewRootDirectoryRecord(segment),
	}

	return &record
}

type DirectoryRecord struct {
	record encoding.DirectoryRecord
}

func overlayDirectoryRecord(r encoding.DirectoryRecord) DirectoryRecord {
	record := DirectoryRecord{
		record: r,
	}
	return record
}

func (record *DirectoryRecord) Filename() string {
	return record.record.Filename()
}

func (record *DirectoryRecord) SetFilename(name string) {
	record.record.SetFilename(name)
}

func (record *DirectoryRecord) Type() ObjectType {
	return ObjectType(record.record.Type())
}

func (record *DirectoryRecord) SetType(t ObjectType) {
	record.record.SetType(uint8(t))
}

func (record *DirectoryRecord) ID() ObjectKey {
	return overlayObjectKey(record.record.Id())
}

func (record *DirectoryRecord) SetID(key ObjectKey) {
	record.record.SetId(key.key)
}

func (record *DirectoryRecord) Size() uint64 {
	return record.record.Size()
}

func (record *DirectoryRecord) SetSize(s uint64) {
	record.record.SetSize(s)
}

func (record *DirectoryRecord) ModificationTime() Time {
	return Time(record.record.ModificationTime())
}

func (record *DirectoryRecord) SetModificationTime(t Time) {
	record.record.SetModificationTime(uint64(t))
}

func (record *DirectoryRecord) CreationTime() Time {
	return Time(record.record.CreationTime())
}

func (record *DirectoryRecord) SetCreationTime(t Time) {
	record.record.SetCreationTime(uint64(t))
}

func (record *DirectoryRecord) Permissions() uint8 {
	return record.record.Permissions()
}

func (record *DirectoryRecord) SetPermissions(p uint8) {
	record.record.SetPermissions(p)
}

func (record *DirectoryRecord) Owner() UID {
	return UID(record.record.Owner())
}

func (record *DirectoryRecord) SetOwner(u UID) {
	record.record.SetOwner(uint8(u))
}

func (record *DirectoryRecord) Group() GID {
	return GID(record.record.Group())
}

func (record *DirectoryRecord) SetGroup(g GID) {
	record.record.SetGroup(uint8(g))
}

func (record *DirectoryRecord) ExtendedAttributes() ObjectKey {
	return overlayObjectKey(record.record.ExtendedAttributes())
}

func (record *DirectoryRecord) SetExtendedAttributes(key ObjectKey) {
	record.record.SetExtendedAttributes(key.key)
}

func NewMultiBlockFile() *MultiBlockFile {
	segment := capn.NewBuffer(nil)
	mb := MultiBlockFile{
		mb: encoding.NewRootMultiBlockFile(segment),
	}

	return &mb
}

func OverlayMultiBlockFile(emb encoding.MultiBlockFile) MultiBlockFile {
	mb := MultiBlockFile{
		mb: emb,
	}
	return mb
}

type MultiBlockFile struct {
	mb encoding.MultiBlockFile
}

func (mb *MultiBlockFile) BlockSize() uint32 {
	return mb.mb.BlockSize()
}

func (mb *MultiBlockFile) SetBlockSize(n uint32) {
	mb.mb.SetBlockSize(n)
}

func (mb *MultiBlockFile) SizeOfLastBlock() uint32 {
	return mb.mb.SizeOfLastBlock()
}

func (mb *MultiBlockFile) SetSizeOfLastBlock(n uint32) {
	mb.mb.SetSizeOfLastBlock(n)
}

func (mb *MultiBlockFile) SetNumberOfBlocks(n int) {
	mb.mb.SetNumberOfBlocks(uint32(n))
}

func (mb *MultiBlockFile) ListOfBlocks() []ObjectKey {
	blocks := mb.mb.ListOfBlocks().ToArray()

	keys := make([]ObjectKey, 0, len(blocks))
	for _, block := range blocks {
		keys = append(keys, overlayObjectKey(block))
	}

	return keys
}

func (mb *MultiBlockFile) SetListOfBlocks(keys []ObjectKey) {
	for i, key := range keys {
		mb.mb.ListOfBlocks().Set(i, key.key)
	}
}

func (mb *MultiBlockFile) Bytes() []byte {
	return mb.mb.Segment.Data
}

func NewVeryLargeFile() *VeryLargeFile {
	segment := capn.NewBuffer(nil)
	vlf := VeryLargeFile{
		vlf: encoding.NewRootVeryLargeFile(segment),
	}

	return &vlf
}

func OverlayVeryLargeFile(evlf encoding.VeryLargeFile) VeryLargeFile {
	vlf := VeryLargeFile{
		vlf: evlf,
	}
	return vlf
}

type VeryLargeFile struct {
	vlf encoding.VeryLargeFile
}

func (vlf *VeryLargeFile) NumberOfParts() int {
	return int(vlf.vlf.NumberOfParts())
}

func (vlf *VeryLargeFile) SetNumberOfParts(n int) {
	vlf.vlf.SetNumberOfParts(uint32(n))
}

func (vlf *VeryLargeFile) LargeFileKey(i int) ObjectKey {
	return overlayObjectKey(vlf.vlf.LargeFileKeys().At(i))
}

func (vlf *VeryLargeFile) SetLargeFileKey(i int, key ObjectKey) {
	vlf.vlf.LargeFileKeys().Set(i, key.key)
}

func (vlf *VeryLargeFile) Bytes() []byte {
	return vlf.vlf.Segment.Data
}

type Buffer interface {
	Write(c *Ctx, in []byte, offset uint32) uint32
	Read(out []byte, offset uint32) int
	Get() []byte
	Set(data []byte, keyType KeyType)
	ContentHash() [ObjectKeyLength - 1]byte
	Key(c *Ctx) (ObjectKey, error)
	SetSize(size int)
	Size() int

	// These methods interpret the Buffer as various metadata types
	AsWorkspaceRoot() WorkspaceRoot
	AsDirectoryEntry() DirectoryEntry
	AsMultiBlockFile() MultiBlockFile
	AsVeryLargeFile() VeryLargeFile
}

type DataStore interface {
	Get(c *Ctx, key ObjectKey, buf Buffer) error
	Set(c *Ctx, key ObjectKey, buf Buffer) error
	Exists(c *Ctx, key ObjectKey) bool
}

// A pseudo-store which contains all the constant objects
var constStore = newConstantStore()
var ConstantStore = DataStore(constStore)

func newConstantStore() *ConstDataStore {
	return &ConstDataStore{
		store: make(map[string][]byte),
	}
}

type ConstDataStore struct {
	store map[string][]byte
}

func (store *ConstDataStore) Get(c *Ctx, key ObjectKey, buf Buffer) error {
	if data, ok := store.store[key.String()]; ok {
		buf.Set(data, key.Type())
		return nil
	}
	return fmt.Errorf("Object not found")
}

func (store *ConstDataStore) Set(c *Ctx, key ObjectKey, buf Buffer) error {
	return fmt.Errorf("Cannot set in constant datastore")
}

func (store *ConstDataStore) Exists(c *Ctx, key ObjectKey) bool {
	return false
}

func init() {
	emptyDirKey := createEmptyDirectory()
	emptyBlockKey := createEmptyBlock()
	emptyWorkspaceKey := createEmptyWorkspace(emptyDirKey)
	EmptyDirKey = emptyDirKey
	EmptyBlockKey = emptyBlockKey
	EmptyWorkspaceKey = emptyWorkspaceKey
}

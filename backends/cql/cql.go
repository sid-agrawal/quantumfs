// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package cql

import (
	"container/list"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/qlog"
	"github.com/aristanetworks/quantumfs/utils"
	"github.com/aristanetworks/quantumfs/utils/simplebuffer"
)

// APIStatsReporter reports statistics like latency, rate etc
//  when the statistics are maintained in memory rather than
//  external statistics stores (eg: InfluxDB)
//  This is an optional interface
type APIStatsReporter interface {
	ReportAPIStats()
}

// ExtKeyInfo is extended CQL store specific
// information for a key
type ExtKeyInfo struct {
	TTL       int       // seconds of TTL
	WriteTime time.Time // last time key's value was written
}

// CqlStore interface is a collection of methods
// which are specific to a blobstore that supports
// CQL protocol.
type CqlStore interface {
	// Keyspace for the store
	Keyspace() string
	// Get extended information like TTL, WriteTime etc
	GetExtKeyInfo(c ctx, key []byte) (ExtKeyInfo, error)
}

// The JSON decoder, by default, doesn't unmarshal time.Duration from a
// string. The custom struct allows to setup an unmarshaller which uses
// time.ParseDuration
type TTLDuration struct {
	Duration time.Duration
}

func (d *TTLDuration) UnmarshalJSON(data []byte) error {
	var str string
	var dur time.Duration
	var err error
	if err = json.Unmarshal(data, &str); err != nil {
		return err
	}

	dur, err = time.ParseDuration(str)
	if err != nil {
		return err
	}

	d.Duration = dur
	return nil
}

type cqlAdapterConfig struct {
	// CqlTTLRefreshTime controls when a block's TTL is refreshed
	// A block's TTL is refreshed when its TTL is <= CqlTTLRefreshTime
	// ttlrefreshtime is a string accepted by
	// https://golang.org/pkg/time/#ParseDuration
	TTLRefreshTime TTLDuration `json:"ttlrefreshtime"`

	// CqlTTLRefreshValue is the time by which a block's TTL will
	// be advanced during TTL refresh.
	// When a block's TTL is refreshed, its new TTL is set as
	// CqlTTLRefreshValue
	// ttlrefreshvalue is a string accepted by
	// https://golang.org/pkg/time/#ParseDuration
	TTLRefreshValue TTLDuration `json:"ttlrefreshvalue"`

	// CqlTTLDefaultValue is the TTL value of a new block
	// When a block is written its TTL is set to
	// CqlTTLDefaultValue
	// ttldefaultvalue is a string accepted by
	// https://golang.org/pkg/time/#ParseDuration
	TTLDefaultValue TTLDuration `json:"ttldefaultvalue"`
}

var refreshTTLTimeSecs int64
var refreshTTLValueSecs int64
var defaultTTLValueSecs int64

func loadCqlAdapterConfig(path string) error {
	var c struct {
		A cqlAdapterConfig `json:"adapter"`
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&c)
	if err != nil {
		return err
	}

	refreshTTLTimeSecs = int64(c.A.TTLRefreshTime.Duration.Seconds())
	refreshTTLValueSecs = int64(c.A.TTLRefreshValue.Duration.Seconds())
	defaultTTLValueSecs = int64(c.A.TTLDefaultValue.Duration.Seconds())

	if refreshTTLTimeSecs == 0 || refreshTTLValueSecs == 0 ||
		defaultTTLValueSecs == 0 {
		return fmt.Errorf("ttldefaultvalue, ttlrefreshvalue and " +
			"ttlrefreshtime must be non-zero")
	}

	// we can add more checks here later on eg: min of 1 day etc

	return nil
}

func NewCqlStore(path string) quantumfs.DataStore {
	cerr := loadCqlAdapterConfig(path)
	if cerr != nil {
		fmt.Printf("Error loading %q: %v\n", path, cerr)
		return nil
	}

	blobstore, err := NewCqlBlobStore(path)
	if err != nil {
		fmt.Printf("Failed to init cql datastore: %s\n",
			err.Error())
		return nil
	}

	translator := CqlBlobStoreTranslator{
		Blobstore:      blobstore,
		ApplyTTLPolicy: true,
		ttlCache:       make(map[string]time.Time),
	}
	return &translator
}

// CqlBlobStoreTranslator translates quantumfs.Datastore APIs
// to Blobstore APIs
//
// NOTE: This is an exported type since some clients currently
// alter the ApplyTTLPolicy attribute. Eventually TTL handling
// will move outside of the adapter into Cql and then this type
// can be turned back into an exported type
type CqlBlobStoreTranslator struct {
	Blobstore      BlobStore
	ApplyTTLPolicy bool

	// The TTL cache uses a FIFO eviction policy in an attempt to keep the TTLs
	// updated. The average cost of resetting unnecessarily is low so little
	// efficiency will be lost.
	ttlCacheLock utils.DeferableRwMutex
	ttlCache     map[string]time.Time
	ttlFifo      list.List // Back is most recently added
}

// asserts that metadata is !nil and it contains TimeToLive
// once Metadata API is refactored, above assertions will be
// revisited

// TTL will be set using Insert under following scenarios:
//  a) key exists and current TTL < refreshTTLTimeSecs
//  b) key doesn't exist
func refreshTTL(c *quantumfs.Ctx, b BlobStore,
	keyExist bool, key []byte, metadata map[string]string,
	buf []byte) error {

	setTTL := defaultTTLValueSecs

	if keyExist {
		if metadata == nil {
			return fmt.Errorf("Store must have metadata")
		}
		ttl, ok := metadata[TimeToLive]
		if !ok {
			return fmt.Errorf("Store must return metadata with " +
				"TimeToLive")
		}
		ttlVal, err := strconv.ParseInt(ttl, 10, 64)
		if err != nil {
			return fmt.Errorf("Invalid TTL value in metadata %s ",
				ttl)
		}

		// if key exists and TTL doesn't need to be refreshed
		// then return
		if ttlVal >= refreshTTLTimeSecs {
			return nil
		}
		// if key exists but TTL needs to be refreshed then
		// calculate new TTL. We don't need to re-fetch the
		// data since data in buf has to be same, given the key
		// exists
		setTTL = refreshTTLValueSecs
	}

	// if key doesn't exist then use default TTL
	newmetadata := make(map[string]string)
	newmetadata[TimeToLive] = fmt.Sprintf("%d", setTTL)
	return b.Insert((*dsApiCtx)(c), key, buf, newmetadata)
}

const maxTtlCacheSize = 1000000
const CqlTtlCacheEvict = "Expiring ttl cache entry"

// This function must be called after any possible refreshTTL() call to ensure the
// block will not expire for the period it is alive in the cache.
func (ebt *CqlBlobStoreTranslator) cacheTtl(c *quantumfs.Ctx, key string) {
	if refreshTTLTimeSecs <= 0 {
		return
	}

	defer ebt.ttlCacheLock.Lock().Unlock()

	for ebt.ttlFifo.Len() >= maxTtlCacheSize {
		c.Dlog(qlog.LogDatastore, CqlTtlCacheEvict)
		toRemove := ebt.ttlFifo.Remove(ebt.ttlFifo.Front()).(string)
		delete(ebt.ttlCache, toRemove)
	}

	cacheDuration := time.Duration(refreshTTLTimeSecs / 2)
	expiry := time.Now().Add(cacheDuration * time.Second)
	ebt.ttlCache[key] = expiry
	ebt.ttlFifo.PushBack(key)
}

const CqlGetLog = "CqlBlobStoreTranslator::Get"
const KeyLog = "Key: %s"

// Get adpats quantumfs.DataStore's Get API to BlobStore.Get
func (ebt *CqlBlobStoreTranslator) Get(c *quantumfs.Ctx,
	key quantumfs.ObjectKey, buf quantumfs.Buffer) error {

	defer c.StatsFuncIn(qlog.LogDatastore, CqlGetLog, KeyLog,
		key.String()).Out()
	kv := key.Value()
	data, metadata, err := ebt.Blobstore.Get((*dsApiCtx)(c), kv)
	if err != nil {
		return err
	}

	if ebt.ApplyTTLPolicy {
		err = refreshTTL(c, ebt.Blobstore, true, kv, metadata, data)
		if err != nil {
			return err
		}
	}

	ebt.cacheTtl(c, key.String())

	newData := make([]byte, len(data))
	copy(newData, data)
	buf.Set(newData, key.Type())
	return nil
}

func (ebt *CqlBlobStoreTranslator) cachedTtlGood(c *quantumfs.Ctx,
	key string) bool {

	defer ebt.ttlCacheLock.RLock().RUnlock()
	expiry, cached := ebt.ttlCache[key]
	if cached && time.Now().Before(expiry) {
		c.Dlog(qlog.LogDatastore, CqlTtlCacheHit)
		return true
	}
	c.Dlog(qlog.LogDatastore, CqlTtlCacheMiss)
	return false
}

const CqlSetLog = "CqlBlobStoreTranslator::Set"
const CqlTtlCacheHit = "CqlBlobStoreTranslator TTL cache hit"
const CqlTtlCacheMiss = "CqlBlobStoreTranslator TTL cache miss"

// Set adapts quantumfs.DataStore's Set API to BlobStore.Insert
func (ebt *CqlBlobStoreTranslator) Set(c *quantumfs.Ctx, key quantumfs.ObjectKey,
	buf quantumfs.Buffer) error {

	kv := key.Value()
	ks := key.String()

	defer c.StatsFuncIn(qlog.LogDatastore, CqlSetLog, KeyLog, ks).Out()

	if buf.Size() > quantumfs.MaxBlockSize {
		err := fmt.Errorf("Key %s size:%d is bigger than max block size",
			ks, buf.Size())
		c.Elog(qlog.LogDatastore, "Failed datastore set: %s",
			err.Error())
		return err
	}

	if ebt.cachedTtlGood(c, ks) {
		return nil
	}

	metadata, err := ebt.Blobstore.Metadata((*dsApiCtx)(c), kv)

	switch {
	case err != nil && err.(*Error).Code == ErrKeyNotFound:
		err = refreshTTL(c, ebt.Blobstore, false, kv, nil, buf.Get())
		if err != nil {
			return err
		}

		ebt.cacheTtl(c, ks)
		return nil

	case err == nil:
		err = refreshTTL(c, ebt.Blobstore, true, kv, metadata, buf.Get())
		if err != nil {
			return err
		}

		ebt.cacheTtl(c, ks)
		return nil

	case err != nil && err.(*Error).Code != ErrKeyNotFound:
		// if metadata error other than ErrKeyNotFound then fail
		// the Set since we haven't been able to ascertain TTL state
		// so we can't overwrite it
		return err
	}

	panicMsg := fmt.Sprintf("CqlAdapter.Set code shouldn't reach here. "+
		"Key %s error %v metadata %v\n", ks, err, metadata)
	panic(panicMsg)
}

func (ebt *CqlBlobStoreTranslator) Freshen(c *quantumfs.Ctx,
	key quantumfs.ObjectKey) error {

	defer c.FuncInName(qlog.LogDatastore,
		"CqlBlobStoreTranslator::Freshen").Out()

	if ebt.cachedTtlGood(c, key.String()) {
		c.Vlog(qlog.LogDatastore, "Freshened block found in TTL cache")
		return nil
	}

	buf := simplebuffer.New([]byte{}, key)
	err := ebt.Get(c, key, buf)
	if err != nil {
		return fmt.Errorf("Cannot freshen %s, block missing from db",
			key.String())
	}
	return ebt.Set(c, key, buf)
}

type cqlWsdbTranslator struct {
	wsdb WorkspaceDB
	lock utils.DeferableRwMutex
}

func NewCqlWorkspaceDB(path string) quantumfs.WorkspaceDB {
	eWsdb := &cqlWsdbTranslator{
		wsdb: NewWorkspaceDB(path),
	}

	// CreateWorkspace is safe to be called 2 by clients of eWsdb in parallel,
	// since both will write the same end value.
	// TODO(sid): Since qfs does not provide a context here, used
	// DefaultCtx. If the higher layers init the Ctx before
	// initializing the backends, this can be solved.
	err := eWsdb.wsdb.CreateWorkspace(DefaultCtx,
		quantumfs.NullSpaceName, quantumfs.NullSpaceName,
		quantumfs.NullSpaceName, quantumfs.WorkspaceNonceInvalid,
		quantumfs.EmptyWorkspaceKey.Value())
	if err != nil {
		panic(fmt.Sprintf("Failed wsdb setup: %s", err.Error()))
	}

	return eWsdb
}

func (w *cqlWsdbTranslator) NumTypespaces(c *quantumfs.Ctx) (int, error) {
	defer c.FuncInName(qlog.LogWorkspaceDb,
		"CqlWsdbTranslator::NumTypespaces").Out()
	defer w.lock.RLock().RUnlock()

	count, err := w.wsdb.NumTypespaces((*wsApiCtx)(c))
	if err != nil {
		return 0, err
	}
	return count, nil
}

const CqlTypespaceLog = "CqlWsdbTranslator::TypespaceList"

func (w *cqlWsdbTranslator) TypespaceList(
	c *quantumfs.Ctx) ([]string, error) {

	defer c.StatsFuncInName(qlog.LogWorkspaceDb, CqlTypespaceLog).Out()
	defer w.lock.RLock().RUnlock()

	list, err := w.wsdb.TypespaceList((*wsApiCtx)(c))
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (w *cqlWsdbTranslator) NumNamespaces(c *quantumfs.Ctx,
	typespace string) (int, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"CqlWsdbTranslator::NumNamespaces",
		"typespace: %s", typespace).Out()
	defer w.lock.RLock().RUnlock()

	count, err := w.wsdb.NumNamespaces((*wsApiCtx)(c), typespace)
	if err != nil {
		return 0, err
	}
	return count, nil
}

const CqlNamespaceLog = "CqlWsdbTranslator::NamespaceList"
const CqlNamespaceDebugLog = "typespace: %s"

func (w *cqlWsdbTranslator) NamespaceList(c *quantumfs.Ctx,
	typespace string) ([]string, error) {

	defer c.StatsFuncIn(qlog.LogWorkspaceDb, CqlNamespaceLog,
		CqlNamespaceDebugLog, typespace).Out()
	defer w.lock.RLock().RUnlock()

	list, err := w.wsdb.NamespaceList((*wsApiCtx)(c), typespace)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (w *cqlWsdbTranslator) NumWorkspaces(c *quantumfs.Ctx,
	typespace string, namespace string) (int, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"CqlWsdbTranslator::NumWorkspaces",
		"%s/%s", typespace, namespace).Out()
	defer w.lock.RLock().RUnlock()

	count, err := w.wsdb.NumWorkspaces((*wsApiCtx)(c), typespace, namespace)
	if err != nil {
		return 0, err
	}
	return count, nil
}

const CqlWorkspaceListLog = "CqlWsdbTranslator::WorkspaceList"
const CqlWorkspaceListDebugLog = "%s/%s"

func (w *cqlWsdbTranslator) WorkspaceList(c *quantumfs.Ctx,
	typespace string, namespace string) (map[string]quantumfs.WorkspaceNonce,
	error) {

	defer c.StatsFuncIn(qlog.LogWorkspaceDb, CqlWorkspaceListLog,
		CqlWorkspaceListDebugLog, typespace, namespace).Out()
	defer w.lock.RLock().RUnlock()

	list, err := w.wsdb.WorkspaceList((*wsApiCtx)(c), typespace, namespace)
	if err != nil {
		return nil, err
	}
	result := make(map[string]quantumfs.WorkspaceNonce, len(list))
	for name := range list {
		result[name] = quantumfs.WorkspaceNonce{
			Id:          uint64(list[name].Id),
			PublishTime: uint64(list[name].PublishTime),
		}
	}
	return result, nil
}

const CqlWorkspaceLog = "CqlWsdbTranslator::Workspace"
const CqlWorkspaceDebugLog = "%s/%s/%s"

func (w *cqlWsdbTranslator) Workspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string) (quantumfs.ObjectKey,
	quantumfs.WorkspaceNonce, error) {

	defer c.StatsFuncIn(qlog.LogWorkspaceDb, CqlWorkspaceLog,
		CqlWorkspaceDebugLog, typespace, namespace, workspace).Out()
	defer w.lock.RLock().RUnlock()

	key, nonce, err := w.wsdb.Workspace((*wsApiCtx)(c), typespace,
		namespace, workspace)
	if err != nil {
		return quantumfs.ZeroKey, quantumfs.WorkspaceNonce{},
			err
	}

	qfsNonce := quantumfs.WorkspaceNonce{
		Id:          uint64(nonce.Id),
		PublishTime: uint64(nonce.PublishTime),
	}
	return quantumfs.NewObjectKeyFromBytes(key), qfsNonce, nil
}

func (w *cqlWsdbTranslator) FetchAndSubscribeWorkspace(c *quantumfs.Ctx,
	typespace string, namespace string, workspace string) (
	quantumfs.ObjectKey, quantumfs.WorkspaceNonce, error) {

	err := w.SubscribeTo(typespace + "/" + namespace + "/" + workspace)
	if err != nil {
		return quantumfs.ZeroKey, quantumfs.WorkspaceNonce{}, err
	}

	return w.Workspace(c, typespace, namespace, workspace)
}

const CqlBranchLog = "CqlWsdbTranslator::BranchWorkspace"
const CqlBranchDebugLog = "%s/%s/%s -> %s/%s/%s"

func (w *cqlWsdbTranslator) BranchWorkspace(c *quantumfs.Ctx, srcTypespace string,
	srcNamespace string, srcWorkspace string, dstTypespace string,
	dstNamespace string, dstWorkspace string) error {

	defer c.StatsFuncIn(qlog.LogWorkspaceDb, CqlBranchLog, CqlBranchDebugLog,
		srcTypespace, srcNamespace, srcWorkspace,
		dstTypespace, dstNamespace, dstWorkspace).Out()
	defer w.lock.Lock().Unlock()

	_, _, err := w.wsdb.BranchWorkspace((*wsApiCtx)(c), srcTypespace,
		srcNamespace, srcWorkspace, dstTypespace, dstNamespace, dstWorkspace)
	if err != nil {
		return err
	}
	return nil
}

func (w *cqlWsdbTranslator) DeleteWorkspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string) error {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"CqlWsdbTranslator::DeleteWorkspace",
		"%s/%s/%s", typespace, namespace, workspace).Out()
	defer w.lock.Lock().Unlock()

	err := w.wsdb.DeleteWorkspace((*wsApiCtx)(c), typespace, namespace,
		workspace)
	if err != nil {
		return err
	}
	return nil
}

const CqlAdvanceLog = "CqlWsdbTranslator::AdvanceWorkspace"
const CqlAdvanceDebugLog = "%s/%s/%s %s -> %s"

func (w *cqlWsdbTranslator) AdvanceWorkspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string, nonce quantumfs.WorkspaceNonce,
	currentRootId quantumfs.ObjectKey,
	newRootId quantumfs.ObjectKey) (quantumfs.ObjectKey,
	quantumfs.WorkspaceNonce, error) {

	defer c.StatsFuncIn(qlog.LogWorkspaceDb, CqlAdvanceLog,
		CqlAdvanceDebugLog, typespace, namespace, workspace,
		currentRootId.String(), newRootId.String()).Out()
	defer w.lock.Lock().Unlock()

	utils.Assert(newRootId.IsValid(), "Tried advancing to an invalid rootID. "+
		"%s/%s/%s %s -> %s", typespace, namespace, workspace,
		currentRootId.String(), newRootId.String())

	wsdbNonce := quantumfs.WorkspaceNonce{
		Id:          nonce.Id,
		PublishTime: nonce.PublishTime,
	}
	key, wsdbNonce, err := w.wsdb.AdvanceWorkspace((*wsApiCtx)(c), typespace,
		namespace, workspace, wsdbNonce, currentRootId.Value(),
		newRootId.Value())
	if err != nil {
		return quantumfs.ZeroKey, quantumfs.WorkspaceNonce{},
			err
	}
	nonce.PublishTime = wsdbNonce.PublishTime

	return quantumfs.NewObjectKeyFromBytes(key), nonce, nil
}

const CqlSetImmutableLog = "CqlWsdbTranslator::SetWorkspaceImmutable"
const CqlSetImmutableDebugLog = "%s/%s/%s"

func (w *cqlWsdbTranslator) SetWorkspaceImmutable(c *quantumfs.Ctx,
	typespace string, namespace string, workspace string) error {

	defer c.FuncIn(qlog.LogWorkspaceDb, CqlSetImmutableLog,
		CqlSetImmutableDebugLog, typespace, namespace, workspace).Out()
	defer w.lock.Lock().Unlock()
	err := w.wsdb.SetWorkspaceImmutable((*wsApiCtx)(c), typespace, namespace,
		workspace)

	if err != nil {
		return err
	}
	return nil
}

const CqlWorkspaceIsImmutableLog = "CqlWsdbTranslator::WorkspaceIsImmutable"
const CqlWorkspaceIsImmutableDebugLog = "%s/%s/%s"

func (w *cqlWsdbTranslator) WorkspaceIsImmutable(c *quantumfs.Ctx,
	typespace string, namespace string, workspace string) (bool, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb, CqlWorkspaceIsImmutableLog,
		CqlWorkspaceIsImmutableDebugLog, typespace, namespace,
		workspace).Out()
	defer w.lock.RLock().RUnlock()
	immutable, err := w.wsdb.WorkspaceIsImmutable((*wsApiCtx)(c), typespace,
		namespace, workspace)

	if err != nil {
		return false, err
	}
	return immutable, nil
}

func (wsdb *cqlWsdbTranslator) SetCallback(
	callback quantumfs.SubscriptionCallback) {
}

func (wsdb *cqlWsdbTranslator) SubscribeTo(workspaceName string) error {
	return nil
}

func (wsdb *cqlWsdbTranslator) UnsubscribeFrom(workspaceName string) {
}

type dsApiCtx quantumfs.Ctx

func (dc *dsApiCtx) Elog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(dc).Elog(qlog.LogDatastore, fmtStr, args...)
}

func (dc *dsApiCtx) Wlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(dc).Wlog(qlog.LogDatastore, fmtStr, args...)
}

func (dc *dsApiCtx) Dlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(dc).Dlog(qlog.LogDatastore, fmtStr, args...)
}

func (dc *dsApiCtx) Vlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(dc).Vlog(qlog.LogDatastore, fmtStr, args...)
}

type cqlFuncOut quantumfs.ExitFuncLog

func (e cqlFuncOut) Out() {
	(quantumfs.ExitFuncLog)(e).Out()
}

func (dc *dsApiCtx) FuncIn(funcName string, fmtStr string,
	args ...interface{}) FuncOut {

	el := (*quantumfs.Ctx)(dc).FuncIn(qlog.LogDatastore, funcName,
		fmtStr, args...)
	return (cqlFuncOut)(el)
}

func (dc *dsApiCtx) FuncInName(funcName string) FuncOut {
	return dc.FuncIn(funcName, "")
}

type wsApiCtx quantumfs.Ctx

func (wc *wsApiCtx) Elog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(wc).Elog(qlog.LogWorkspaceDb, fmtStr, args...)
}

func (wc *wsApiCtx) Wlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(wc).Wlog(qlog.LogWorkspaceDb, fmtStr, args...)
}

func (wc *wsApiCtx) Dlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(wc).Dlog(qlog.LogWorkspaceDb, fmtStr, args...)
}

func (wc *wsApiCtx) Vlog(fmtStr string, args ...interface{}) {
	(*quantumfs.Ctx)(wc).Vlog(qlog.LogWorkspaceDb, fmtStr, args...)
}

func (wc *wsApiCtx) FuncIn(funcName string, fmtStr string,
	args ...interface{}) FuncOut {

	el := (*quantumfs.Ctx)(wc).FuncIn(qlog.LogWorkspaceDb, funcName,
		fmtStr, args...)
	return (cqlFuncOut)(el)
}

func (wc *wsApiCtx) FuncInName(funcName string) FuncOut {
	return wc.FuncIn(funcName, "")
}

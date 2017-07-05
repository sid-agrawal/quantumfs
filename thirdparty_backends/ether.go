// +build !skip_backends

// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package thirdparty_backends

// To avoid compiling in support for ether datastore
// change "!skip_backends" in first line with "ignore"
// You will need to do the same in daemon/Ether_test.go as well.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aristanetworks/ether"
	"github.com/aristanetworks/ether/blobstore"
	"github.com/aristanetworks/ether/cql"
	"github.com/aristanetworks/ether/filesystem"
	"github.com/aristanetworks/ether/qubit/wsdb"
	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/qlog"
	"github.com/aristanetworks/quantumfs/utils"
)

func init() {
	registerDatastore("ether.filesystem", newEtherFilesystemStore)
	registerDatastore("ether.cql", newEtherCqlStore)
	registerWorkspaceDB("ether.cql", newEtherWorkspaceDB)
}

// TODO(krishna) TTL configuration is specific to CQL blobstore.
// However due to current blobstore APIs managing store specific
// metadata in common APIs, TTL metadata is being applied to all
// blobstores managed by ether adapter.
// APIs will be refactored to support store specific interfaces
// for managing store specific metadata
//
// Currently, filesystem datastore doesn't accept a configuration file.
// Hence refreshTTLTimeSecs = refreshTTLValueSecs =  defaultTTLValueSecs = 0
// Hence the TTL metadata defaults to 0. In filesystem
// datastore the TTL on the block doesn't count down and hence TTL is
// actually never refreshed since TTL > refreshTTLTimeSecs (=0) always

func newEtherFilesystemStore(path string) quantumfs.DataStore {

	blobstore, err := filesystem.NewFilesystemStore(path)
	if err != nil {
		fmt.Printf("Failed to init ether.filesystem datastore: %s\n",
			err.Error())
		return nil
	}
	translator := EtherBlobStoreTranslator{Blobstore: blobstore}
	return &translator
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

func newEtherCqlStore(path string) quantumfs.DataStore {
	cerr := loadCqlAdapterConfig(path)
	if cerr != nil {
		fmt.Printf("Error loading %q: %v\n", path, cerr)
		return nil
	}

	blobstore, err := cql.NewCqlBlobStore(path)
	if err != nil {
		fmt.Printf("Failed to init ether.cql datastore: %s\n",
			err.Error())
		return nil
	}

	translator := EtherBlobStoreTranslator{
		Blobstore:      blobstore,
		ApplyTTLPolicy: true,
	}
	return &translator
}

// EtherBlobStoreTranslator translates quantumfs.Datastore APIs
// to ether.Blobstore APIs
//
// NOTE: This is an exported type since some clients currently
// alter the ApplyTTLPolicy attribute. Eventually TTL handling
// will move outside of the adapter into Ether and then this type
// can be turned back into an exported type
type EtherBlobStoreTranslator struct {
	Blobstore      blobstore.BlobStore
	ApplyTTLPolicy bool
}

// asserts that metadata is !nil and it contains cql.TimeToLive
// once Metadata API is refactored, above assertions will be
// revisited

// TTL will be set using Insert under following scenarios:
//  a) key exists and current TTL < refreshTTLTimeSecs
//  b) key doesn't exist
func refreshTTL(c *quantumfs.Ctx, b blobstore.BlobStore,
	keyExist bool, key []byte, metadata map[string]string,
	buf []byte) error {

	setTTL := defaultTTLValueSecs

	if keyExist {
		if metadata == nil {
			return fmt.Errorf("Store must have metadata")
		}
		ttl, ok := metadata[cql.TimeToLive]
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
	newmetadata[cql.TimeToLive] = fmt.Sprintf("%d", setTTL)
	return b.Insert((*dsApiCtx)(c), key, buf, newmetadata)
}

const EtherGetLog = "EtherBlobStoreTranslator::Get"
const KeyLog = "key %s"
// Get adpats quantumfs.DataStore's Get API to ether.BlobStore.Get
func (ebt *EtherBlobStoreTranslator) Get(c *quantumfs.Ctx,
	key quantumfs.ObjectKey, buf quantumfs.Buffer) error {

	defer c.FuncIn(qlog.LogDatastore, EtherGetLog, KeyLog, key.String()).Out()
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

	newData := make([]byte, len(data))
	copy(newData, data)
	buf.Set(newData, key.Type())
	return nil
}

const EtherSetLog = "EtherBlobStoreTranslator::Set"
// Set adpats quantumfs.DataStore's Set API to ether.BlobStore.Insert
func (ebt *EtherBlobStoreTranslator) Set(c *quantumfs.Ctx, key quantumfs.ObjectKey,
	buf quantumfs.Buffer) error {

	kv := key.Value()
	ks := key.String()

	defer c.FuncIn(qlog.LogDatastore, EtherSetLog, KeyLog, ks).Out()

	metadata, err := ebt.Blobstore.Metadata((*dsApiCtx)(c), kv)

	switch {
	case err != nil && err.(*blobstore.Error).Code == blobstore.ErrKeyNotFound:
		return refreshTTL(c, ebt.Blobstore, false, kv, nil, buf.Get())

	case err == nil:
		return refreshTTL(c, ebt.Blobstore, true, kv, metadata, buf.Get())

	case err != nil && err.(*blobstore.Error).Code != blobstore.ErrKeyNotFound:
		// if metadata error other than ErrKeyNotFound then fail
		// the Set since we haven't been able to ascertain TTL state
		// so we can't overwrite it
		return err
	}

	panicMsg := fmt.Sprintf("EtherAdapter.Set code shouldn't reach here. "+
		"Key %s error %v metadata %v\n", ks, err, metadata)
	panic(panicMsg)
}

type etherWsdbTranslator struct {
	wsdb wsdb.WorkspaceDB
}

// convert wsdb.Error to quantumfs.WorkspaceDbErr
func convertWsdbError(e error) error {
	wE, ok := e.(*wsdb.Error)
	if !ok {
		panic("BUG: Errors from wsdb APIs must be of *wsdb.Error type")
	}

	var errCode quantumfs.WsdbErrCode
	switch wE.Code {
	case wsdb.ErrWorkspaceExists:
		errCode = quantumfs.WSDB_WORKSPACE_EXISTS
	case wsdb.ErrWorkspaceNotFound:
		errCode = quantumfs.WSDB_WORKSPACE_NOT_FOUND
	case wsdb.ErrFatal:
		errCode = quantumfs.WSDB_FATAL_DB_ERROR
	case wsdb.ErrWorkspaceOutOfDate:
		errCode = quantumfs.WSDB_OUT_OF_DATE
	case wsdb.ErrLocked:
		errCode = quantumfs.WSDB_LOCKED
	default:
		panic(fmt.Sprintf("Bug: Unsupported error %s", e.Error()))
	}

	return quantumfs.NewWorkspaceDbErr(errCode, wE.Msg)
}

func newEtherWorkspaceDB(path string) quantumfs.WorkspaceDB {
	eWsdb := &etherWsdbTranslator{
		wsdb: cql.NewWorkspaceDB(path),
	}

	// since generic wsdb API sets up _null/null with nil key
	// TODO(sid): Since qfs does not provide a context here, used
	// ether.DefaultCtx. If the higher layers init the Ctx before
	// initializing the backends, this can be solved.
	key, err := eWsdb.wsdb.AdvanceWorkspace(ether.DefaultCtx,
		quantumfs.NullSpaceName, quantumfs.NullSpaceName,
		quantumfs.NullSpaceName, []byte(nil),
		quantumfs.EmptyWorkspaceKey.Value())
	if err != nil {
		// an existing workspaceDB will have currentRootID as
		// EmptyWorkspaceKey
		wE, _ := err.(*wsdb.Error)
		if wE.Code != wsdb.ErrWorkspaceOutOfDate ||
			!bytes.Equal(key, quantumfs.EmptyWorkspaceKey.Value()) {
			panic(fmt.Sprintf("Failed wsdb setup: %s", err.Error()))
		}
	}

	return eWsdb
}

func (w *etherWsdbTranslator) NumTypespaces(c *quantumfs.Ctx) (int, error) {
	defer c.FuncInName(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::NumTypespaces").Out()

	count, err := w.wsdb.NumTypespaces((*wsApiCtx)(c))
	if err != nil {
		return 0, convertWsdbError(err)
	}
	return count, nil
}

const EtherTypespaceLog = "EtherWsdbTranslator::TypespaceList"
func (w *etherWsdbTranslator) TypespaceList(
	c *quantumfs.Ctx) ([]string, error) {

	defer c.FuncInName(qlog.LogWorkspaceDb, EtherTypespaceLog).Out()

	list, err := w.wsdb.TypespaceList((*wsApiCtx)(c))
	if err != nil {
		return nil, convertWsdbError(err)
	}
	return list, nil
}

func (w *etherWsdbTranslator) NumNamespaces(c *quantumfs.Ctx,
	typespace string) (int, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::NumNamespaces",
		"typespace: %s", typespace).Out()

	count, err := w.wsdb.NumNamespaces((*wsApiCtx)(c), typespace)
	if err != nil {
		return 0, convertWsdbError(err)
	}
	return count, nil
}

const EtherNamespaceLog = "EtherWsdbTranslator::NamespaceList"
const EtherNamespaceDebugLog = "typespace: %s"
func (w *etherWsdbTranslator) NamespaceList(c *quantumfs.Ctx,
	typespace string) ([]string, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb, EtherNamespaceLog,
		EtherNamespaceDebugLog, typespace).Out()

	list, err := w.wsdb.NamespaceList((*wsApiCtx)(c), typespace)
	if err != nil {
		return nil, convertWsdbError(err)
	}
	return list, nil
}

func (w *etherWsdbTranslator) NumWorkspaces(c *quantumfs.Ctx,
	typespace string, namespace string) (int, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::NumWorkspaces",
		"%s/%s", typespace, namespace).Out()

	count, err := w.wsdb.NumWorkspaces((*wsApiCtx)(c), typespace, namespace)
	if err != nil {
		return 0, convertWsdbError(err)
	}
	return count, nil
}

const EtherWorkspaceListLog = "EtherWsdbTranslator::WorkspaceList"
const EtherWorkspaceListDebugLog = "%s/%s"
func (w *etherWsdbTranslator) WorkspaceList(c *quantumfs.Ctx,
	typespace string, namespace string) ([]string, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb, EtherWorkspaceListLog,
		EtherWorkspaceListDebugLog, typespace, namespace).Out()

	list, err := w.wsdb.WorkspaceList((*wsApiCtx)(c), typespace, namespace)
	if err != nil {
		return nil, convertWsdbError(err)
	}
	return list, nil
}

func (w *etherWsdbTranslator) TypespaceExists(c *quantumfs.Ctx,
	typespace string) (bool, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::TypespaceExists",
		"typespace: %s", typespace).Out()

	exists, err := w.wsdb.TypespaceExists((*wsApiCtx)(c), typespace)
	if err != nil {
		return exists, convertWsdbError(err)
	}
	return exists, nil
}

func (w *etherWsdbTranslator) NamespaceExists(c *quantumfs.Ctx,
	typespace string, namespace string) (bool, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::NamespaceExists",
		"%s/%s", typespace, namespace).Out()

	exists, err := w.wsdb.NamespaceExists((*wsApiCtx)(c), typespace, namespace)
	if err != nil {
		return exists, convertWsdbError(err)
	}
	return exists, nil
}

func (w *etherWsdbTranslator) WorkspaceExists(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string) (bool, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::WorkspaceExists",
		"%s/%s/%s", typespace, namespace, workspace).Out()

	exists, err := w.wsdb.WorkspaceExists((*wsApiCtx)(c), typespace,
		namespace, workspace)
	if err != nil {
		return exists, convertWsdbError(err)
	}
	return exists, nil
}

const EtherWorkspaceLog = "EtherWsdbTranslator::Workspace"
const EtherWorkspaceDebugLog = "%s/%s/%s"
func (w *etherWsdbTranslator) Workspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string) (quantumfs.ObjectKey, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb, EtherWorkspaceLog,
		EtherWorkspaceDebugLog, typespace, namespace, workspace).Out()

	key, err := w.wsdb.Workspace((*wsApiCtx)(c), typespace, namespace, workspace)
	if err != nil {
		return quantumfs.ObjectKey{}, convertWsdbError(err)
	}

	return quantumfs.NewObjectKeyFromBytes(key), nil
}

const EtherBranchLog = "EtherWsdbTranslator::BranchWorkspace"
const EtherBranchDebugLog = "%s/%s/%s -> %s/%s/%s"
func (w *etherWsdbTranslator) BranchWorkspace(c *quantumfs.Ctx, srcTypespace string,
	srcNamespace string, srcWorkspace string, dstTypespace string,
	dstNamespace string, dstWorkspace string) error {

	defer c.FuncIn(qlog.LogWorkspaceDb, EtherBranchLog, EtherBranchDebugLog,
		srcTypespace, srcNamespace, srcWorkspace,
		dstTypespace, dstNamespace, dstWorkspace).Out()

	err := w.wsdb.BranchWorkspace((*wsApiCtx)(c), srcTypespace, srcNamespace,
		srcWorkspace, dstTypespace, dstNamespace, dstWorkspace)
	if err != nil {
		return convertWsdbError(err)
	}
	return nil
}

func (w *etherWsdbTranslator) DeleteWorkspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string) error {

	defer c.FuncIn(qlog.LogWorkspaceDb,
		"EtherWsdbTranslator::DeleteWorkspace",
		"%s/%s/%s", typespace, namespace, workspace).Out()

	err := w.wsdb.DeleteWorkspace((*wsApiCtx)(c), typespace, namespace,
		workspace)
	if err != nil {
		return convertWsdbError(err)
	}
	return nil
}

const EtherAdvanceLog = "EtherWsdbTranslator::AdvanceWorkspace"
const EtherAdvanceDebugLog = "%s/%s/%s %s -> %s"
func (w *etherWsdbTranslator) AdvanceWorkspace(c *quantumfs.Ctx, typespace string,
	namespace string, workspace string, currentRootId quantumfs.ObjectKey,
	newRootId quantumfs.ObjectKey) (quantumfs.ObjectKey, error) {

	defer c.FuncIn(qlog.LogWorkspaceDb, EtherAdvanceLog, EtherAdvanceDebugLog,
		typespace, namespace, workspace,
		currentRootId.String(),
		newRootId.String()).Out()

	utils.Assert(newRootId.IsValid(), "Tried advancing to an invalid rootID. "+
		"%s/%s/%s %s -> %s", typespace, namespace, workspace,
		currentRootId.String(), newRootId.String())

	key, err := w.wsdb.AdvanceWorkspace((*wsApiCtx)(c), typespace, namespace,
		workspace, currentRootId.Value(), newRootId.Value())
	if err != nil {
		return quantumfs.NewObjectKeyFromBytes(key), convertWsdbError(err)
	}

	return quantumfs.NewObjectKeyFromBytes(key), nil
}

func (w *etherWsdbTranslator) SetWorkspaceImmutable(c *quantumfs.Ctx,
	typespace string, namespace string, workspace string) error {

	return quantumfs.NewWorkspaceDbErr(quantumfs.WSDB_FATAL_DB_ERROR,
		"Ether does not support setting workspaces immutable")
}

func (w *etherWsdbTranslator) WorkspaceIsImmutable(c *quantumfs.Ctx,
	typespace string, namespace string, workspace string) (bool, error) {

	return false, nil
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

type etherFuncOut quantumfs.ExitFuncLog

func (e etherFuncOut) Out() {
	(quantumfs.ExitFuncLog)(e).Out()
}

func (dc *dsApiCtx) FuncIn(funcName string, fmtStr string,
	args ...interface{}) ether.FuncOut {

	el := (*quantumfs.Ctx)(dc).FuncIn(qlog.LogDatastore, funcName,
		fmtStr, args...)
	return (etherFuncOut)(el)
}

func (dc *dsApiCtx) FuncInName(funcName string) ether.FuncOut {
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
	args ...interface{}) ether.FuncOut {

	el := (*quantumfs.Ctx)(wc).FuncIn(qlog.LogWorkspaceDb, funcName,
		fmtStr, args...)
	return (etherFuncOut)(el)
}

func (wc *wsApiCtx) FuncInName(funcName string) ether.FuncOut {
	return wc.FuncIn(funcName, "")
}

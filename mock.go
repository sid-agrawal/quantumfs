// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// NOTE: This file was initially generated using mockery tool but to support
// certain usecases (look for comments with MANUAL tag) the generated code has been modified.
// Additionally this file now includes helper mock routines

package cql

import (
	"reflect"

	mock "github.com/stretchr/testify/mock"
)

// MockCluster is an autogenerated mock type for the Cluster type
type MockCluster struct {
	mock.Mock
}

// CreateSession provides a mock function with given fields:
func (_m *MockCluster) CreateSession() (Session, error) {
	ret := _m.Called()

	var r0 Session
	if rf, ok := ret.Get(0).(func() Session); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(Session)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

var _ Cluster = (*MockCluster)(nil)

// MockSession is an autogenerated mock type for the Session type
type MockSession struct {
	mock.Mock
}

// Close provides a mock function with given fields:
func (_m *MockSession) Close() {
	_m.Called()
}

// Closed provides a mock function with given fields:
func (_m *MockSession) Closed() bool {
	ret := _m.Called()

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// Query provides a mock function with given fields: stmt, values
func (_m *MockSession) Query(stmt string, values ...interface{}) Query {

	ret := _m.Called(stmt, values)

	var r0 Query
	if rf, ok := ret.Get(0).(func(string, ...interface{}) Query); ok {
		r0 = rf(stmt, values...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(Query)
		}
	}

	return r0
}

var _ Session = (*MockSession)(nil)

// MockQuery is an autogenerated mock type for the Query type
type MockQuery struct {
	mock.Mock
}

// Exec provides a mock function with given fields:
func (_m *MockQuery) Exec() error {
	ret := _m.Called()

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// Iter provides a mock function with given fields:
func (_m *MockQuery) Iter() Iter {
	ret := _m.Called()

	var r0 Iter
	if rf, ok := ret.Get(0).(func() Iter); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(Iter)
		}
	}

	return r0
}

// Scan provides a mock function with given fields: dest
func (_m *MockQuery) Scan(dest ...interface{}) error {
	// MANUAL: added the ... so that in the test code we can simply
	// pass in varargs rather than passing in a slice of args
	// improves test code readability
	ret := _m.Called(dest...)

	var r0 error
	if rf, ok := ret.Get(0).(func(...interface{}) error); ok {
		r0 = rf(dest...)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// String provides a mock function with given fields:
func (_m *MockQuery) String() string {
	ret := _m.Called()

	var r0 string
	if rf, ok := ret.Get(0).(func() string); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(string)
	}

	return r0
}

var _ Query = (*MockQuery)(nil)

// mockDbRows represents a slice of rows where
// each row is a slice of columns
// MANUAL
type mockDbRows [][]interface{}

// MockIter is enhanced to support iteration
// over mocked rows with pause support
// MANUAL
type MockIter struct {
	mock.Mock
	currentRow int
	pause      bool
	rows       mockDbRows
}

// Close provides a mock function with given fields:
func (_m *MockIter) Close() error {
	ret := _m.Called()

	var r0 error
	if rf, ok := ret.Get(0).(func() error); ok {
		r0 = rf()
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

// GetCustomPayload provides a mock function with given fields:
func (_m *MockIter) GetCustomPayload() map[string][]byte {
	ret := _m.Called()

	var r0 map[string][]byte
	if rf, ok := ret.Get(0).(func() map[string][]byte); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(map[string][]byte)
		}
	}

	return r0
}

// MapScan provides a mock function with given fields: m
func (_m *MockIter) MapScan(m map[string]interface{}) bool {
	ret := _m.Called(m)

	var r0 bool
	if rf, ok := ret.Get(0).(func(map[string]interface{}) bool); ok {
		r0 = rf(m)
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// NumRows provides a mock function with given fields:
func (_m *MockIter) NumRows() int {
	ret := _m.Called()

	var r0 int
	if rf, ok := ret.Get(0).(func() int); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(int)
	}

	return r0
}

// PageState provides a mock function with given fields:
func (_m *MockIter) PageState() []byte {
	ret := _m.Called()

	var r0 []byte
	if rf, ok := ret.Get(0).(func() []byte); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]byte)
		}
	}

	return r0
}

// Scan provides a mock function with given fields: dest
func (_m *MockIter) Scan(dest ...interface{}) bool {
	// MANUAL: added ... to improve test code readability by using
	// varargs instead of creating a slice of args
	ret := _m.Called(dest...)

	var r0 bool
	if rf, ok := ret.Get(0).(func(...interface{}) bool); ok {
		r0 = rf(dest...)
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// SliceMap provides a mock function with given fields:
func (_m *MockIter) SliceMap() ([]map[string]interface{}, error) {
	ret := _m.Called()

	var r0 []map[string]interface{}
	if rf, ok := ret.Get(0).(func() []map[string]interface{}); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]map[string]interface{})
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func() error); ok {
		r1 = rf()
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// WillSwitchPage provides a mock function with given fields:
func (_m *MockIter) WillSwitchPage() bool {
	ret := _m.Called()

	var r0 bool
	if rf, ok := ret.Get(0).(func() bool); ok {
		r0 = rf()
	} else {
		r0 = ret.Get(0).(bool)
	}

	return r0
}

// Reset the Iter so that it can be used again
// MANUAL
func (_m *MockIter) Reset() {
	_m.currentRow = 0
}

// SetRows setups up the Iter with given rows
// MANUAL
func (_m *MockIter) SetRows(rows mockDbRows) {
	_m.rows = rows
	_m.currentRow = 0
	_m.pause = false
}

var _ Iter = (*MockIter)(nil)

// --- helper mock routines (MANUAL) ---

// a type mismatch between the destination and
// value or a destination which is not a
// pointer kind will cause panic here. Such panic
// is intentional and implies that the mock test
// case needs to be fixed.
func assignValToDest(val interface{}, dest interface{}) {
	destElem := reflect.ValueOf(dest).Elem()
	refVal := reflect.ValueOf(val)
	destElem.Set(refVal)
}

func mockDbTypespaceGet(sess *MockSession, typespace string, err error) {

	query := new(MockQuery)
	qScanFunc := newMockQueryScanStrings(err, []interface{}(nil))
	query.On("Scan", mock.AnythingOfType("*string")).Return(qScanFunc)

	args := []interface{}{typespace}
	sess.On("Query", `
SELECT typespace
FROM ether.workspacedb
WHERE typespace = ? LIMIT 1`, args).Return(query)
}

func mockDbNamespaceGet(sess *MockSession, typespace string,
	namespace string, err error) {

	query := new(MockQuery)
	qScanFunc := newMockQueryScanStrings(err, []interface{}(nil))
	query.On("Scan", mock.AnythingOfType("*string")).Return(qScanFunc)

	args := []interface{}{typespace, namespace}
	sess.On("Query", `
SELECT namespace
FROM ether.workspacedb
WHERE typespace = ? AND namespace = ? LIMIT 1`, args).Return(query)
}

func mockWsdbKeyDel(sess *MockSession, typespace string,
	namespace string, workspace string, err error) {

	query := new(MockQuery)
	stmt := `
DELETE
FROM ether.workspacedb
WHERE typespace=? AND namespace=? AND workspace=?`
	sess.On("Query", stmt,
		[]interface{}{typespace, namespace, workspace}).Return(query)
	if err == nil {
		query.On("Exec").Return(nil)
	} else {
		query.On("Exec").Return(err)
	}
}

func mockWsdbKeyGet(sess *MockSession, typespace string,
	namespace string, workspace string, key []byte, err error) {

	query := new(MockQuery)
	qScanFunc := newMockQueryScanByteSlice(err, key)
	query.On("Scan", mock.AnythingOfType("*[]uint8")).Return(qScanFunc)

	args := []interface{}{typespace, namespace, workspace}

	sess.On("Query", `
SELECT key
FROM ether.workspacedb
WHERE typespace = ? AND namespace = ? AND workspace = ?`, args).Return(query)
}

func mockWsdbKeyPut(sess *MockSession, typespace string,
	namespace string, workspace string, key []byte, err error) {

	query := new(MockQuery)
	stmt := `
INSERT INTO ether.workspacedb
(typespace, namespace, workspace, key)
VALUES (?,?,?,?)`

	sess.On("Query", stmt,
		[]interface{}{typespace, namespace, workspace, key}).Return(query)
	if err == nil {
		query.On("Exec").Return(nil)
	} else {
		query.On("Exec").Return(err)
	}
}

func newMockQueryScanByteSlice(err error,
	val []byte) func(dest ...interface{}) error {

	return func(dest ...interface{}) error {

		if err == nil {
			// byte slice pointed by val is copied
			// into dest[0]
			assignValToDest(val, dest[0])
		}
		return err
	}
}

func newMockQueryScanStrings(err error,
	vals []interface{}) func(dest ...interface{}) error {

	return func(dest ...interface{}) error {

		if err == nil {
			for i := range vals {
				assignValToDest(vals[i], dest[i])
			}
		}
		return err
	}
}

func newMockIterScan(fetchPause chan bool,
	iter *MockIter) func(dest ...interface{}) bool {

	if fetchPause != nil {
		iter.pause = true
	} else {
		iter.pause = false
	}

	return func(dest ...interface{}) bool {
		if iter.currentRow >= len(iter.rows) {
			// simulates a DB delay or block after fetching all rows
			if iter.pause && fetchPause != nil {
				fetchPause <- true
				<-fetchPause
			}
			return false
		}
		vals := iter.rows[iter.currentRow]
		for i := range vals {
			assignValToDest(vals[i], dest[i])
		}
		iter.currentRow++
		return true
	}
}

func setupMockWsdbCacheCqlFetch(sess *MockSession, iter *MockIter,
	stmt string, rows mockDbRows, vals []interface{},
	fetchPause chan bool) {

	iter.On("Close").Return(nil)
	iter.SetRows(rows)
	iterateRows := newMockIterScan(fetchPause, iter)
	iter.On("Scan", mock.AnythingOfType("*string")).Return(iterateRows)

	fetchQuery := new(MockQuery)
	fetchQuery.On("Iter").Return(iter)

	sess.On("Query", stmt, vals).Return(fetchQuery)
}

func mockWsdbCacheTypespaceFetchPanic(sess *MockSession) {
	iter := new(MockIter)
	raisePanic := func(dest ...interface{}) bool { panic("PanicOnFetch") }
	iter.On("Scan", mock.AnythingOfType("*string")).Return(raisePanic)

	fetchQuery := new(MockQuery)
	fetchQuery.On("Iter").Return(iter)

	sess.On("Query", `
SELECT distinct typespace
FROM ether.workspacedb`, []interface{}(nil)).Return(fetchQuery)

}

func mockWsdbCacheTypespaceFetch(sess *MockSession,
	rows mockDbRows, vals []interface{},
	iter *MockIter, fetchPause chan bool) {

	setupMockWsdbCacheCqlFetch(sess, iter, `
SELECT distinct typespace
FROM ether.workspacedb`, rows, vals, fetchPause)
}

func mockWsdbCacheNamespaceFetch(sess *MockSession,
	rows mockDbRows, vals []interface{},
	iter *MockIter, fetchPause chan bool) {

	setupMockWsdbCacheCqlFetch(sess, iter, `
SELECT namespace
FROM ether.workspacedb
WHERE typespace = ?`, rows, vals, fetchPause)
}

func mockWsdbCacheWorkspaceFetch(sess *MockSession,
	rows mockDbRows, vals []interface{},
	iter *MockIter, fetchPause chan bool) {

	setupMockWsdbCacheCqlFetch(sess, iter, `
SELECT workspace
FROM ether.workspacedb
WHERE typespace = ? AND namespace = ?`, rows, vals, fetchPause)

}

func mockBranchWorkspace(sess *MockSession, srcTypespace string,
	srcNamespace string, srcWorkspace string,
	dstTypespace string, dstNamespace string, dstWorkspace string,
	srcKey []byte, dstErr error) {

	mockWsdbKeyGet(sess, srcTypespace, srcNamespace, srcWorkspace,
		srcKey, nil)
	mockWsdbKeyGet(sess, dstTypespace, dstNamespace, dstWorkspace,
		nil, dstErr)
	mockWsdbKeyPut(sess, dstTypespace, dstNamespace, dstWorkspace,
		srcKey, nil)
}

// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

import "arista.com/quantumfs"
import "arista.com/quantumfs/qlog"
import "github.com/hanwen/go-fuse/fuse"

// The ctx type needs to be threaded through all the objects and calls of the
// system. It provides access to the configuration, the quantumfs mux and request
// specific logging information.
//
// If Go ever gets goroutine local storage it may be cleaner to move these contents
// to using that instead of threading it everywhere.
type ctx struct {
	quantumfs.Ctx
	qfs          *QuantumFs
	config       *QuantumFsConfig
	workspaceDB  quantumfs.WorkspaceDB
	durableStore quantumfs.DataStore
	fuseCtx      *fuse.Context
}

func (c *ctx) req(header *fuse.InHeader) *ctx {
	requestCtx := &ctx{
		Ctx: quantumfs.Ctx{
			Qlog:      c.Qlog,
			RequestId: header.Unique,
		},
		qfs:          c.qfs,
		config:       c.config,
		workspaceDB:  c.workspaceDB,
		durableStore: c.durableStore,
		fuseCtx:      &header.Context,
	}
	return requestCtx
}

//only to be used for some testing - not all functions will work with this
func (c *ctx) dummyReq(request uint64) *ctx {
	requestCtx := &ctx{
		Ctx: quantumfs.Ctx{
			Qlog:      c.Qlog,
			RequestId: c.RequestId,
		},
		qfs:          c.qfs,
		config:       c.config,
		workspaceDB:  c.workspaceDB,
		durableStore: c.durableStore,
		fuseCtx:      nil,
	}
	return requestCtx
}

// local daemon package specific log wrappers
func (c ctx) elog(format string, args ...interface{}) {
	c.Qlog.Log(qlog.LogDaemon, c.RequestId, 0, format, args...)
}

func (c ctx) wlog(format string, args ...interface{}) {
	c.Qlog.Log(qlog.LogDaemon, c.RequestId, 1, format, args...)
}

func (c ctx) dlog(format string, args ...interface{}) {
	c.Qlog.Log(qlog.LogDaemon, c.RequestId, 2, format, args...)
}

func (c ctx) vlog(format string, args ...interface{}) {
	c.Qlog.Log(qlog.LogDaemon, c.RequestId, 3, format, args...)
}

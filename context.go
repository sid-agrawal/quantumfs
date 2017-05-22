// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.
package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"golang.org/x/net/context"

	"github.com/aristanetworks/ether/blobstore"
	influxlib "github.com/aristanetworks/influxlib/go"
	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/qlog"
	"github.com/aristanetworks/quantumfs/thirdparty_backends"
	qubitutils "github.com/aristanetworks/qubit/tools/utils"
)

var requestID uint64

// Ctx maintains context for the walker daemon.
type Ctx struct {
	context.Context
	Influx     *influxlib.InfluxDBConnection
	qctx       *quantumfs.Ctx
	wsdb       quantumfs.WorkspaceDB
	ds         quantumfs.DataStore
	cqlds      blobstore.BlobStore
	ttlCfg     *qubitutils.TTLConfig
	confFile   string
	numSuccess uint32
	numError   uint32
	numWalkers int
	iteration  uint
}

func getWalkerDaemonContext(influxServer string, influxPort int,
	influxDBName string, config string, logdir string, numwalkers int) *Ctx {

	// Connect to InfluxDB
	var influx *influxlib.InfluxDBConnection
	var err error
	if influxServer == "" {
		influx, err = influxlib.Connect(nil)
		if err != nil {
			fmt.Printf("Unable to connect to default influxDB err:%v\n", err)
			os.Exit(1)
		}
	} else {
		influxConfig := influxlib.DefaultConfig()
		influxConfig.Hostname = influxServer
		influxConfig.Port = influxPort
		influxConfig.Database = influxDBName

		influx, err = influxlib.Connect(influxConfig)
		if err != nil {
			fmt.Printf("Unable to connect to influxDB at addr %v:%v and db:%v err:%v\n",
				influxServer, influxPort, influxDBName, err)
			os.Exit(exitBadConfig)
		}
	}

	// Connect to ether backed quantumfs DataStore
	quantumfsDS, err := thirdparty_backends.ConnectDatastore("ether.cql", config)
	if err != nil {
		fmt.Printf("Connection to DataStore failed")
		os.Exit(exitBadConfig)
	}

	// Extract blobstore from quantumfs DataStore
	var cqlDS blobstore.BlobStore
	if v, ok := quantumfsDS.(*thirdparty_backends.EtherBlobStoreTranslator); ok {
		cqlDS = v.Blobstore
		v.ApplyTTLPolicy = false
	}

	// Connect to ether.cql WorkSpaceDB
	quantumfsWSDB, err := thirdparty_backends.ConnectWorkspaceDB("ether.cql", config)
	if err != nil {
		fmt.Printf("Connection to workspaceDB failed err: %v\n", err)
		os.Exit(exitBadConfig)
	}

	// Load TTL Config values
	ttlConfig, err := qubitutils.LoadTTLConfig(config)
	if err != nil {
		fmt.Printf("Failed to load TTL: %s\n", err.Error())
		os.Exit(exitBadConfig)
	}

	// Obtain a qlog instance
	log := qlog.NewQlogTiny()
	if logdir != "" {
		log = qlog.NewQlog(logdir)
	}

	id := atomic.AddUint64(&requestID, 1)
	return &Ctx{
		Influx:     influx,
		qctx:       newQCtx(log, id),
		wsdb:       quantumfsWSDB,
		ds:         quantumfsDS,
		cqlds:      cqlDS,
		ttlCfg:     ttlConfig,
		confFile:   config,
		numWalkers: numwalkers,
	}
}

func newQCtx(log *qlog.Qlog, id uint64) *quantumfs.Ctx {
	c := &quantumfs.Ctx{
		Qlog:      log,
		RequestId: id,
	}
	log.SetLogLevels("Tool/*")
	return c
}

func (c *Ctx) newRequestID() *Ctx {

	id := atomic.AddUint64(&requestID, 1)
	return &Ctx{
		Context:    c.Context,
		Influx:     c.Influx,
		qctx:       newQCtx(c.qctx.Qlog, id),
		wsdb:       c.wsdb,
		ds:         c.ds,
		cqlds:      c.cqlds,
		ttlCfg:     c.ttlCfg,
		confFile:   c.confFile,
		numSuccess: c.numSuccess,
		numError:   c.numError,
		numWalkers: c.numWalkers,
		iteration:  c.iteration,
	}
}

// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package main

import (
	"strconv"
	"time"
)

type wsDetails struct {
	ts     string
	ns     string
	ws     string
	rootID string
}

// WriteWorkspaceWalkDuration is a measurement point writer
// tags:   typeSpace - Typespace for the quantumfs workspace
//         nameSpace - Namespace for the quantumfs workspace
//         pass      - Walk failed or passed
//	   keyspace  - Keyspace of the WorkspaceDB
//
// fields: workSpace - Name of the workspace (text)
//         walkTimeSec - Time it took to walk the workspace in
//                       seconds (uint)
//         rootID    - rootID for the workSpace
///
func WriteWorkspaceWalkDuration(c *Ctx, w wsDetails, pass bool,
	dur time.Duration) {

	measurement := "workspaceWalkDuration"
	tags := map[string]string{
		"typeSpace": w.ts,
		"nameSpace": w.ns,
		"pass":      strconv.FormatBool(pass),
		"keyspace":  c.keyspace,
	}
	fields := map[string]interface{}{
		"workSpace":   w.ws,
		"walkTimeSec": uint(dur / time.Second),
		"iteration":   c.iteration,
		"rootID":      w.rootID,
	}

	err := c.Influx.WritePoint(measurement, tags, fields)
	if err != nil {
		c.elog("Writing %s to influxDB for "+
			"%s/%s/%s (%s) iteration=%v walkSuccess=%v err:%v\n",
			measurement, w.ts, w.ns, w.ws, w.rootID,
			c.iteration, pass, err)
		return
	}
	c.vlog("%s Writing %s=%v to influxDB for "+
		"%s/%s/%s (%s) iteration=%v walkSuccess=%v \n",
		successPrefix, measurement, dur,
		w.ts, w.ns, w.ws, w.rootID,
		c.iteration, pass)
}

// WriteWalkerIteration is a measurement point writer
//
// tags:   none
//
// fields: walkTimeMin - Time it took to walk all the workspace in
//                       minutes (uint)
//         countSuccess - Num successful walks
//         countError   - Num failed walks
//
func WriteWalkerIteration(c *Ctx, dur time.Duration,
	numSuccess uint32, numError uint32) {

	measurement := "walkerIteration"
	tags := map[string]string{
		"keyspace": c.keyspace,
	}
	fields := map[string]interface{}{
		"walkTimeMin":  uint(dur / time.Minute),
		"iteration":    c.iteration,
		"countSuccess": numSuccess,
		"countError":   numError,
	}
	err := c.Influx.WritePoint(measurement, tags, fields)
	if err != nil {
		c.elog("Writing %s iteration=%v to influxDB err: %v\n",
			measurement, c.iteration, err)
		return
	}
	c.vlog("%s Writing %s=%v iteration=%v numSuccess=%v numError=%v to influxDB\n",
		successPrefix, measurement, dur, c.iteration, numSuccess, numError)
}

// Write point to indicate that walker is alive.
//
// tags:   none
//
// fields: alive - a place holder. Since, we have to have a field.
//
func WriteWalkerHeartBeat(c *Ctx) {

	measurement := "walkerHeartBeat"
	tags := map[string]string{
		"keyspace": c.keyspace,
	}
	fields := map[string]interface{}{
		"alive": 1,
	}

	err := c.Influx.WritePoint(measurement, tags, fields)
	if err != nil {
		c.elog("Error:   Writing %s to influxDB err: %v\n", measurement, err)
		return
	}
	c.vlog("Success: Writing %s to influxDB\n", measurement)
}

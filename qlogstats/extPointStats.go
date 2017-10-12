// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// extPointStats is a stat extractor that watches a singular log

package qlogstats

import (
	"github.com/aristanetworks/quantumfs"
	"github.com/aristanetworks/quantumfs/qlog"
)

type extPointStats struct {
	format   string
	name     string
	messages chan *qlog.LogOutput

	stats basicStats
}

func NewExtPointStats(format_ string, nametag string) *extPointStats {
	ext := &extPointStats{
		format:   format_ + "\n",
		name:     nametag,
		messages: make(chan *qlog.LogOutput, 10000),
	}

	go ext.process()

	return ext
}

func (ext *extPointStats) TriggerStrings() []string {
	rtn := make([]string, 0)

	rtn = append(rtn, ext.format)
	return rtn
}

func (ext *extPointStats) Chan() chan *qlog.LogOutput {
	return ext.messages
}

func (ext *extPointStats) Type() TriggerType {
	return OnFormat
}

func (ext *extPointStats) process() {
	for {
		log := <-ext.messages
		ext.stats.NewPoint(int64(log.T))
	}
}

func (ext *extPointStats) Publish() (measurement string, tags []quantumfs.Tag,
	fields []quantumfs.Field) {

	tags = make([]quantumfs.Tag, 0)
	tags = append(tags, quantumfs.NewTag("statName", ext.name))

	fields = make([]quantumfs.Field, 0)

	fields = append(fields, quantumfs.NewField("samples", ext.stats.Count()))

	ext.stats = basicStats{}
	return "quantumFsPointCount", tags, fields
}

func (ext *extPointStats) GC() {
	// We keep no state, so there is nothing to do
}

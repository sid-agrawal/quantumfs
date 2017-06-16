// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package qlogstats

import (
	"testing"
	"time"

	"github.com/aristanetworks/quantumfs/processlocal"
	"github.com/aristanetworks/quantumfs/qlog"
)

func runReader(qlogFile string,
	extractors []StatExtractorConfig) *processlocal.Memdb {

	db := processlocal.NewMemdb()
	agg := AggregateLogs(qlog.ReadOnly, qlogFile, db, extractors)

	agg.requestEndAfter = time.Millisecond * 100

	return db
}

func TestMatches(t *testing.T) {
	runTest(t, func(test *testHelper) {
		qlogHandle := test.Logger

		// Artificially insert matching logs.
		// The average of 10,000 and 30,000 should be 20,000
		duration1 := int64(10000)
		duration2 := int64(30000)
		qlogHandle.Log_(time.Unix(0, 20000), qlog.LogTest, 12345, 2,
			qlog.FnEnterStr+"TestMatch")
		qlogHandle.Log_(time.Unix(0, 20000+duration1), qlog.LogTest, 12345,
			3, qlog.FnExitStr+"TestMatch")

		qlogHandle.Log_(time.Unix(0, 50000), qlog.LogTest, 12346, 3,
			qlog.FnEnterStr+"TestMatch")
		qlogHandle.Log_(time.Unix(0, 50000+duration2), qlog.LogTest, 12346,
			3, qlog.FnExitStr+"TestMatch")

		// Add in some close, but not actually matching logs
		qlogHandle.Log(qlog.LogTest, 12345, 2, qlog.FnEnterStr+"TestMatchZ")
		qlogHandle.Log(qlog.LogTest, 12345, 3, qlog.FnExitStr+"TestMtch")
		qlogHandle.Log(qlog.LogTest, 12345, 3, "TestMatch")
		qlogHandle.Log(qlog.LogTest, 12347, 3, qlog.FnExitStr+"TestMatch")

		// Setup an extractor
		extractors := []StatExtractorConfig{
			NewStatExtractorConfig(
				NewExtPairStats(qlog.FnEnterStr+"TestMatch\n",
					qlog.FnExitStr+"TestMatch\n", true,
					"TestMatch"),
				300*time.Millisecond),
		}

		// Run the reader
		memdb := runReader(test.CachePath+"/ramfs/qlog", extractors)

		test.WaitFor("statistic to register", func() bool {
			if len(memdb.Data) == 0 {
				return false
			}

			if len(memdb.Data[0].Fields) == 0 {
				return false
			}

			test.Assert(len(memdb.Data[0].Fields) == 6,
				"%d fields produced from one matching log",
				len(memdb.Data[0].Fields))

			// Check if we're too early
			for _, v := range memdb.Data[0].Fields {
				if v.Name == "samples" && v.Data == 0 {
					return false
				}
			}

			// Data should be present now
			for _, v := range memdb.Data[0].Fields {
				if v.Name == "average" {
					test.Assert(v.Data == 20000,
						"incorrect delta %d", v.Data)
				} else if v.Name == "samples" {
					test.Assert(v.Data == 2,
						"incorrect samples %d", v.Data)
				}
			}

			return true
		})
	})
}

func TestPercentiles(t *testing.T) {
	runTest(t, func(test *testHelper) {
		qlogHandle := test.Logger

		// Artificially insert matching with sensible percentiles
		base := int64(200000)
		// Reverse the order to ensure we test that sorting is working
		for i := int64(100); i >= 0; i-- {
			qlogHandle.Log_(time.Unix(0, base), qlog.LogTest,
				uint64(base), 2, qlog.FnEnterStr+"TestMatch")
			qlogHandle.Log_(time.Unix(0, base+i), qlog.LogTest,
				uint64(base), 2, qlog.FnExitStr+"TestMatch")
			base += int64(i)
		}

		// Setup an extractor
		extractors := []StatExtractorConfig{
			NewStatExtractorConfig(
				NewExtPairStats(qlog.FnEnterStr+"TestMatch\n",
					qlog.FnExitStr+"TestMatch\n", true,
					"TestMatch"),
				300*time.Millisecond),
		}

		// Run the reader
		memdb := runReader(test.CachePath+"/ramfs/qlog", extractors)

		test.WaitFor("statistic to register", func() bool {
			if len(memdb.Data) == 0 {
				return false
			}

			if len(memdb.Data[0].Fields) == 0 {
				return false
			}

			test.Assert(len(memdb.Data[0].Fields) == 6,
				"%d fields produced from one matching log",
				len(memdb.Data[0].Fields))

			// Check if we're too early
			for _, v := range memdb.Data[0].Fields {
				if v.Name == "samples" && v.Data == 0 {
					return false
				}
			}

			// Data should be present now
			for _, v := range memdb.Data[0].Fields {
				if v.Name == "average" {
					test.Assert(v.Data == 50,
						"incorrect delta %d", v.Data)
				} else if v.Name == "samples" {
					test.Assert(v.Data == 101,
						"incorrect samples %d", v.Data)
				} else if v.Name == "50pct" {
					test.Assert(v.Data == 50,
						"50th percentile is %d", v.Data)
				} else if v.Name == "90pct" {
					test.Assert(v.Data == 90,
						"90th percentile is %d", v.Data)
				} else if v.Name == "95pct" {
					test.Assert(v.Data == 95,
						"95th percentile is %d", v.Data)
				} else if v.Name == "99pct" {
					test.Assert(v.Data == 99,
						"99th percentile is %d", v.Data)
				}
			}

			return true
		})
	})
}

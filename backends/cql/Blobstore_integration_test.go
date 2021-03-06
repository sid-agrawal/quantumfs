// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

// The set of tests in this file are for long running
// tests of cql package against a live scylladb.

// +build longrunningtests

package cql

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
)

type storeIntegrationTests struct {
	suite.Suite
	bls BlobStore
}

func checkSetupInteg(s *storeIntegrationTests) {
	if s.bls == nil {
		s.T().Skip("Blobstore was not setup")
	}
}

func (s *storeIntegrationTests) SetupSuite() {
	confFile, err := CqlConfFile()
	s.Require().NoError(err, "error in getting cql configuration file")

	err = SetupIntegTestKeyspace(confFile)
	s.Require().NoError(err, "SetupIntegTestKeyspace returned an error")
	err = DoTestSchemaOp(confFile, SchemaCreate)
	s.Require().NoError(err, "DoTestSchemaOp SchemaCreate returned an error")

	// Establish connection with the cluster
	s.bls, err = NewCqlBlobStore(confFile)
	s.Require().NoError(err, "NewCqlBlobStore returned an error")
	s.Require().NotNil(s.bls, "NewCqlBlobStore returned nil")
}

func (s *storeIntegrationTests) SetupTest() {
	checkSetupInteg(s)
}

func (s *storeIntegrationTests) TestSchemaCheckFailed() {
	confFile, err := CqlConfFile()
	s.Require().NoError(err, "error in getting cql configuration file")

	// setup a dummy CFNAME_PREFIX so that the session
	// establishment fails since no such table exists
	oldCfName := os.Getenv("CFNAME_PREFIX")
	os.Setenv("CFNAME_PREFIX", "dummyPrefix")
	_, err = NewCqlBlobStore(confFile)

	s.Require().Error(err, "NewCqlBlobStore should have failed")
	s.Require().Contains(err.Error(), "dummyPrefix")
	os.Setenv("CFNAME_PREFIX", oldCfName)
}

func (s *storeIntegrationTests) TestInsert() {
	err := s.bls.Insert(integTestCqlCtx, []byte(testKey), []byte(testValue),
		map[string]string{TimeToLive: "0"})
	s.Require().NoError(err, "Insert returned an error")
}

func (s *storeIntegrationTests) TestInsertParallel() {
	c := context.Background()
	Wg, _ := errgroup.WithContext(c)

	for count := 0; count < 2; count++ {
		countl := count
		Wg.Go(func() error {

			return s.bls.Insert(integTestCqlCtx,
				[]byte(testKey+strconv.Itoa(countl)),
				[]byte(testValue),
				map[string]string{TimeToLive: "0"})
		})
	}
	err := Wg.Wait()
	s.Require().NoError(err, "Insert returned an error")

	// Check
	for count := 0; count < 2; count++ {
		value, _, err := s.bls.Get(integTestCqlCtx,
			[]byte(testKey+strconv.Itoa(count)))
		s.Require().NoError(err, "Insert returned an error")
		s.Require().Equal(testValue, string(value),
			"Get returned in correct value")
	}
}

func (s *storeIntegrationTests) TestGet() {
	err := s.bls.Insert(integTestCqlCtx, []byte(testKey), []byte(testValue),
		map[string]string{TimeToLive: "0"})
	s.Require().NoError(err, "Insert returned an error")

	value, metadata, err := s.bls.Get(integTestCqlCtx, []byte(testKey))
	s.Require().NoError(err, "Get returned an error")
	s.Require().Equal(testValue, string(value), "Get returned incorrect value")
	s.Require().NotNil(metadata, "Get returned incorrect metadata")
	s.Require().Contains(metadata, TimeToLive,
		"Metadata doesn't contain expected key TimeToLive")
	s.Require().Equal("0", metadata[TimeToLive],
		"Metadata contains unexpected value for TimeToLive")
}

func (s *storeIntegrationTests) TestGetUnknownKey() {
	value, metadata, err := s.bls.Get(integTestCqlCtx, []byte(unknownKey))
	s.Require().Nil(value, "value was not Nil when error is ErrKeyNotFound")
	s.Require().Nil(metadata,
		"metadata was not Nil when error is ErrKeyNotFound")
	s.Require().Error(err, "Get returned incorrect error")
	verr, ok := err.(*Error)
	s.Require().Equal(true, ok, fmt.Sprintf("Error from Get is of type %T", err))
	s.Require().Equal(ErrKeyNotFound, verr.Code, "Invalid Error Code from Get")
}

func (s *storeIntegrationTests) TestGetNonZeroTTL() {
	err := s.bls.Insert(integTestCqlCtx, []byte(testKey), []byte(testValue),
		map[string]string{TimeToLive: "1234"})
	s.Require().NoError(err, "Insert returned an error")

	value, metadata, err := s.bls.Get(integTestCqlCtx, []byte(testKey))
	s.Require().NoError(err, "Get returned an error")
	s.Require().Equal(testValue, string(value), "Get returned incorrect value")
	s.Require().NotNil(metadata, "Get returned incorrect metadata")
	s.Require().Contains(metadata, TimeToLive,
		"Metadata doesn't contain expected key TimeToLive")
	s.Require().Condition(func() bool {
		i, err := strconv.Atoi(metadata[TimeToLive])
		if err != nil || i <= 0 || i > 1234 {
			return false
		}
		return true
	},
		"Metadata contains expected:%s actual:%s for TimeToLive",
		"0<TTL<=1234", metadata[TimeToLive])
}

func (s *storeIntegrationTests) TestMetadataOK() {
	err := s.bls.Insert(integTestCqlCtx, []byte(testKey), []byte(testValue),
		map[string]string{TimeToLive: "1234"})
	s.Require().NoError(err, "Insert returned an error")

	metadata, err := s.bls.Metadata(integTestCqlCtx, []byte(testKey))
	s.Require().NoError(err, "Metadata returned an error")
	s.Require().NotNil(metadata, "Metadata returned incorrect metadata")
	s.Require().Contains(metadata, TimeToLive,
		"Metadata doesn't contain expected key TimeToLive")
	s.Require().Condition(func() bool {
		i, err := strconv.Atoi(metadata[TimeToLive])
		if err != nil || i <= 0 || i > 1234 {
			return false
		}
		return true
	},
		"Metadata contains expected:%s actual:%s for TimeToLive",
		"0<TTL<=1234", metadata[TimeToLive])
}

func (s *storeIntegrationTests) TestMetadataUnknownKey() {
	metadata, err := s.bls.Metadata(integTestCqlCtx, []byte(unknownKey))
	s.Require().Error(err, "Metadata didn't return error")
	s.Require().Nil(metadata,
		"metadata was not Nil when error is ErrKeyNotFound")
	verr, ok := err.(*Error)
	s.Require().Equal(true, ok,
		fmt.Sprintf("Error from Metadata is of type %T", err))
	s.Require().Equal(ErrKeyNotFound, verr.Code,
		"Invalid Error Code from Metadata")
}

func TestStoreInteg(t *testing.T) {
	suite.Run(t, &storeIntegrationTests{})
}

// The TearDownTest method will be run after every test in the suite.
func (s *storeIntegrationTests) TearDownTest() {
}

// The TearDownSuite method will be run after Suite is done
func (s *storeIntegrationTests) TearDownSuite() {

	confFile, err := CqlConfFile()
	s.Require().NoError(err, "error in getting cql configuration file")

	resetCqlStore()
	_ = DoTestSchemaOp(confFile, SchemaDelete)
}

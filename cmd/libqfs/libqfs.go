// Copyright (c) 2018 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// package name: libqfs
package main

import "C"

import (
	"github.com/aristanetworks/quantumfs/libqfs"
)

//export FindApiPath
func FindApiPath() (string, string) {
	errStr := ""
	rtn, err := libqfs.FindApiPath()
	if err != nil {
		errStr = err.Error()
	}

	return rtn, errStr
}

func main() {
	// Main function must exist for this to be compiled as a shared library
}

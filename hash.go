// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

// The hashing framework
package quantumfs

import "crypto/sha1"

/*
#cgo LDFLAGS: /usr/local/lib/libcityhash.a

#include <stdint.h>
#include <stddef.h>

void cCityHash128(const char *s, size_t len, uint64_t *lower, uint64_t *upper);
void cCityHashCrc256(const char *s, size_t len, uint64_t *result);
*/
import "C"
import "unsafe"

const hashSize = sha1.Size


func Hash(input []byte) [hashSize]byte {
	//return sha1.Sum(input)
	// HACK to test
	hash := CityHash256(input)
	var rtn [hashSize]byte
	copy(rtn[:], hash[:hashSize])
	return rtn
}

// CityHash wrapper
func CityHash128(input []byte) [16]byte {
	var hash [16]byte	
	C.cCityHash128((*C.char)(unsafe.Pointer(&input[0])), C.size_t(len(input)),
		(*C.uint64_t)(unsafe.Pointer(&hash[0])),
		(*C.uint64_t)(unsafe.Pointer(&hash[8])))
	return hash
}

func CityHash256(input []byte) [32]byte {
	if len(input) == 0 {
		// Note: generated by passing length zero to cCityHashCrc256
		return [32]byte{ 0x30, 0xf9, 0xa5, 0xe6, 0x24, 0x2f, 0x16, 0x95,
			0xe0, 0x06, 0xeb, 0xf1, 0xf4, 0xbd, 0x08, 0x68, 0x82, 0x4d,
			0x62, 0x7b, 0xa6, 0xf3, 0xb1, 0xb3, 0x0b, 0xd8, 0x4c, 0xbd,
			0x12, 0x2f, 0xa6, 0xc9 }
	}

	var hash [32]byte
	C.cCityHashCrc256((*C.char)(unsafe.Pointer(&input[0])), C.size_t(len(input)),
		(*C.uint64_t)(unsafe.Pointer(&hash[0])))
	return hash
}

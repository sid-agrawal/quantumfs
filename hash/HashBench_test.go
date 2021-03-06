// Copyright (c) 2016 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

package hash

import (
	"crypto/md5"
	"crypto/sha1"
	"testing"
)

func genData(length int) []byte {
	rtn := make([]byte, length)
	for i := 0; i < len(rtn); i++ {
		rtn[i] = byte(i)
	}
	return rtn
}

const shortLength = 12
const mediumLength = 1024
const longLength = 1048576

func BenchmarkSha1Short(test *testing.B) {
	data := genData(shortLength)

	for i := 0; i < test.N; i++ {
		sha1.Sum(data)
	}
}

func BenchmarkSha1Medium(test *testing.B) {
	data := genData(mediumLength)

	for i := 0; i < test.N; i++ {
		sha1.Sum(data)
	}
}

func BenchmarkSha1Long(test *testing.B) {
	data := genData(longLength)

	for i := 0; i < test.N; i++ {
		sha1.Sum(data)
	}
}

func BenchmarkCity128Short(test *testing.B) {
	data := genData(shortLength)

	for i := 0; i < test.N; i++ {
		cityHash128(data)
	}
}

func BenchmarkCity128Medium(test *testing.B) {
	data := genData(mediumLength)

	for i := 0; i < test.N; i++ {
		cityHash128(data)
	}
}

func BenchmarkCity128Long(test *testing.B) {
	data := genData(longLength)

	for i := 0; i < test.N; i++ {
		cityHash128(data)
	}
}

func BenchmarkCity256CrcShort(test *testing.B) {
	data := genData(shortLength)

	for i := 0; i < test.N; i++ {
		cityHash256(data)
	}
}

func BenchmarkCity256CrcMedium(test *testing.B) {
	data := genData(mediumLength)

	for i := 0; i < test.N; i++ {
		cityHash256(data)
	}
}

func BenchmarkCity256CrcLong(test *testing.B) {
	data := genData(longLength)

	for i := 0; i < test.N; i++ {
		cityHash256(data)
	}
}

func BenchmarkMd5Short(test *testing.B) {
	data := genData(shortLength)

	for i := 0; i < test.N; i++ {
		md5.Sum(data)
	}
}

func BenchmarkMd5Medium(test *testing.B) {
	data := genData(mediumLength)

	for i := 0; i < test.N; i++ {
		md5.Sum(data)
	}
}

func BenchmarkMd5Long(test *testing.B) {
	data := genData(longLength)

	for i := 0; i < test.N; i++ {
		md5.Sum(data)
	}
}

// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package utils

import (
	"syscall"
	"unsafe"
)

// NOTE: The content of this file have been copied from golang's syscall package
// and then have been modified to parse the offset of the directory entries.

const isBigEndian = false

func readIntBE(b []byte, size uintptr) uint64 {
	switch size {
	case 1:
		return uint64(b[0])
	case 2:
		_ = b[1] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[1]) | uint64(b[0])<<8
	case 4:
		_ = b[3] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[3]) | uint64(b[2])<<8 |
			uint64(b[1])<<16 | uint64(b[0])<<24
	case 8:
		_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[7]) | uint64(b[6])<<8 |
			uint64(b[5])<<16 | uint64(b[4])<<24 |
			uint64(b[3])<<32 | uint64(b[2])<<40 |
			uint64(b[1])<<48 | uint64(b[0])<<56
	default:
		panic("syscall: readInt with unsupported size")
	}
}

func readIntLE(b []byte, size uintptr) uint64 {
	switch size {
	case 1:
		return uint64(b[0])
	case 2:
		_ = b[1] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[0]) | uint64(b[1])<<8
	case 4:
		_ = b[3] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[0]) | uint64(b[1])<<8 |
			uint64(b[2])<<16 | uint64(b[3])<<24
	case 8:
		_ = b[7] // bounds check hint to compiler; see golang.org/issue/14808
		return uint64(b[0]) | uint64(b[1])<<8 |
			uint64(b[2])<<16 | uint64(b[3])<<24 |
			uint64(b[4])<<32 | uint64(b[5])<<40 |
			uint64(b[6])<<48 | uint64(b[7])<<56
	default:
		panic("syscall: readInt with unsupported size")
	}
}

func readInt(b []byte, off, size uintptr) (u uint64, ok bool) {
	if len(b) < int(off+size) {
		return 0, false
	}
	if isBigEndian {
		return readIntBE(b[off:], size), true
	}
	return readIntLE(b[off:], size), true
}

func direntReclen(buf []byte) (uint64, bool) {
	return readInt(buf, unsafe.Offsetof(syscall.Dirent{}.Reclen),
		unsafe.Sizeof(syscall.Dirent{}.Reclen))
}

func direntIno(buf []byte) (uint64, bool) {
	return readInt(buf, unsafe.Offsetof(syscall.Dirent{}.Ino),
		unsafe.Sizeof(syscall.Dirent{}.Ino))
}

func direntNamlen(buf []byte) (uint64, bool) {
	reclen, ok := direntReclen(buf)
	if !ok {
		return 0, false
	}
	return reclen - uint64(unsafe.Offsetof(syscall.Dirent{}.Name)), true
}

func direntOffset(buf []byte) (int64, bool) {
	offset, ok := readInt(buf, unsafe.Offsetof(syscall.Dirent{}.Off),
		unsafe.Sizeof(syscall.Dirent{}.Off))
	return int64(offset), ok
}

func ParseDirentOffsets(buf []byte, max int, offs []int64) (consumed int,
	count int, newoffs []int64) {

	origlen := len(buf)
	count = 0
	for max != 0 && len(buf) > 0 {
		reclen, ok := direntReclen(buf)
		if !ok || reclen > uint64(len(buf)) {
			return origlen, count, offs
		}
		rec := buf[:reclen]
		buf = buf[reclen:]
		ino, ok := direntIno(rec)
		if !ok {
			break
		}
		if ino == 0 { // File absent in directory.
			continue
		}

		// Extract the name so that . and .. can be ignored
		const namoff = uint64(unsafe.Offsetof(syscall.Dirent{}.Name))
		namlen, ok := direntNamlen(rec)
		if !ok || namoff+namlen > uint64(len(rec)) {
			break
		}
		name := rec[namoff : namoff+namlen]
		for i, c := range name {
			if c == 0 {
				name = name[:i]
				break
			}
		}
		// Check for useless names before allocating a string.
		if string(name) == "." || string(name) == ".." {
			continue
		}

		off, ok := direntOffset(rec)
		if !ok {
			break
		}
		max--
		count++
		offs = append(offs, off)
	}
	return origlen - len(buf), count, offs
}

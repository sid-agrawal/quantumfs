// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test the various operations on files such as creation, read and write

import "syscall"
import "testing"
import "os"
import "bytes"
import "io"
import "fmt"

import "arista.com/quantumfs"

func TestFileCreation_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.startDefaultQuantumFs()

		workspace := quantumfs.NullNamespaceName + "/" +
			quantumfs.NullWorkspaceName
		testFilename := workspace + "/" + "test"
		fd, err := syscall.Creat(test.relPath(testFilename), 0124)
		test.assert(err == nil, "Error creating file: %v", err)

		err = syscall.Close(fd)
		test.assert(err == nil, "Error closing fd: %v", err)

		var stat syscall.Stat_t
		err = syscall.Stat(test.relPath(testFilename), &stat)
		test.assert(err == nil, "Error stat'ing test file: %v", err)
		test.assert(stat.Size == 0, "Incorrect Size: %d", stat.Size)
		test.assert(stat.Nlink == 1, "Incorrect Nlink: %d", stat.Nlink)

		var expectedPermissions uint32
		expectedPermissions |= syscall.S_IFREG
		expectedPermissions |= syscall.S_IRWXU | syscall.S_IRWXG |
			syscall.S_IRWXO
		test.assert(stat.Mode == expectedPermissions,
			"File permissions incorrect. Expected %x got %x",
			expectedPermissions, stat.Mode)
	})
}

func TestFileReadWrite_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.startDefaultQuantumFs()

		//length of test Text should be 37, and not a multiple of readBuf len
		testText := []byte("This is test data 1234567890 !@#^&*()")
		//write the test data in two goes
		textSplit := len(testText) / 2

		workspace := quantumfs.NullNamespaceName + "/" +
			quantumfs.NullWorkspaceName
		testFilename := workspace + "/" + "testrw"
		file, err := os.Create(test.relPath(testFilename))
		test.assert(file != nil && err == nil,
			"Error creating file: %v", err)

		//ensure the Create() handle works
		written := 0
		for written < textSplit {
			var writeIt int
			writeIt, err = file.Write(testText[written:textSplit])
			written += writeIt
			test.assert(err == nil, "Error writing to new fd: %v", err)
		}

		readLen := 0
		//intentionally make the read buffer small so we do multiple reads
		fullReadBuf := make([]byte, 100)
		readBuf := make([]byte, 4)
		for readLen < written {
			var readIt int
			//note, this also tests read offsets
			readIt, err = file.ReadAt(readBuf, int64(readLen))
			copy(fullReadBuf[readLen:], readBuf[:readIt])
			readLen += readIt
			test.assert(err == nil || err == io.EOF,
				"Error reading from fd: %v", err)
		}
		test.assert(bytes.Equal(fullReadBuf[:readLen], testText[:written]),
			fmt.Sprintf("Read and written bytes do not match, %s vs %s",
				fullReadBuf[:readLen], testText[:written]))

		err = file.Close()
		test.assert(err == nil, "Error closing fd: %v", err)

		//now open the file again to trigger Open()
		file, err = os.OpenFile(test.relPath(testFilename),
			os.O_RDWR, 0777)
		test.assert(err == nil, "Error opening fd: %v", err)

		//test overwriting past the end of the file with an offset by
		//rewinding back the textSplit
		textSplit -= 2
		written -= 2

		//ensure the Open() handle works
		for written < len(testText) {
			var writeIt int
			//test the offset code path by writing a small bit at a time
			writeTo := len(testText)
			if writeTo > written+4 {
				writeTo = written + 4
			}

			writeIt, err = file.WriteAt(testText[written:writeTo],
				int64(written))
			written += writeIt
			test.assert(err == nil, "Error writing existing fd: %v", err)
		}

		readLen = 0
		for readLen < written {
			var readIt int
			//note, this also tests read offsets
			readIt, err = file.ReadAt(readBuf, int64(readLen))
			copy(fullReadBuf[readLen:], readBuf[:readIt])
			readLen += readIt
			test.assert(err == nil || err == io.EOF,
				"Error reading from fd: %v", err)
		}
		test.assert(bytes.Equal(fullReadBuf[:readLen], testText[:written]),
			fmt.Sprintf("Read and written bytes do not match, %s vs %s",
				fullReadBuf[:readLen], testText[:written]))

		err = file.Close()
		test.assert(err == nil, "Error closing fd: %v", err)

		file, err = os.OpenFile(test.relPath(testFilename), os.O_RDWR, 0777)
		test.assert(err == nil, "Error opening fd: %v", err)

		readLen = 0
		for readLen < len(testText) {
			var readIt int
			readIt, err = file.Read(readBuf)
			copy(fullReadBuf[readLen:], readBuf[:readIt])
			readLen += readIt
			test.assert(err != io.EOF || err == nil,
				"Error reading from fd: %v", err)
		}
		test.assert(bytes.Equal(fullReadBuf[:readLen], testText[:written]),
			fmt.Sprintf("Read and written bytes do not match, %s vs %s",
				fullReadBuf[:readLen], testText[:written]))

		err = file.Close()
		test.assert(err == nil, "Error closing fd: %v", err)
	})
}

func TestFileDescriptorDirtying_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.startDefaultQuantumFs()

		// Create a file and determine its inode numbers
		workspace := quantumfs.NullNamespaceName + "/" +
			quantumfs.NullWorkspaceName
		testFilename := workspace + "/" + "test"
		fd, err := syscall.Creat(test.relPath(testFilename), 0124)
		test.assert(err == nil, "Error creating file: %v", err)
		var stat syscall.Stat_t
		err = syscall.Stat(test.relPath(testFilename), &stat)
		test.assert(err == nil, "Error stat'ind test file: %v", err)
		test.assert(stat.Ino >= quantumfs.InodeIdReservedEnd,
			"File had reserved inode number %d", stat.Ino)

		// Find the matching FileHandle
		descriptors := test.fileDescriptorFromInodeNum(stat.Ino)
		test.assert(len(descriptors) == 1,
			"Incorrect number of fds found 1 != %d", len(descriptors))
		fileDescriptor := descriptors[0]
		file := fileDescriptor.file

		// Save the workspace rootId, change the File key, simulating
		// changing the data, then mark the matching FileDescriptor dirty.
		// This should trigger a refresh up the hierarchy and, because we
		// currently do not support delayed syncing, change the workspace
		// rootId and mark the fileDescriptor clean.
		oldRootId := test.workspaceRootId(quantumfs.NullNamespaceName,
			quantumfs.NullWorkspaceName)

		file.key.Key[1]++
		fileDescriptor.dirty(test.newCtx())

		newRootId := test.workspaceRootId(quantumfs.NullNamespaceName,
			quantumfs.NullWorkspaceName)

		test.assert(oldRootId != newRootId, "Workspace rootId didn't change")
		test.assert(!file.dirty_, "FileDescriptor not cleaned after"+
			"change")

		syscall.Close(fd)
	})
}

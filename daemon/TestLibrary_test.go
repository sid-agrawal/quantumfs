// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test library

import "bufio"
import "fmt"
import "io/ioutil"
import "os"
import "runtime"
import "runtime/debug"
import "strings"
import "strconv"
import "sync"
import "sync/atomic"
import "testing"
import "time"

import "arista.com/quantumfs"
import "arista.com/quantumfs/processlocal"
import "arista.com/quantumfs/qlog"

import "github.com/hanwen/go-fuse/fuse"

const fusectlPath = "/sys/fs/fuse/"

type quantumFsTest func(test *testHelper)

// startTest is a helper which configures the testing environment
func runTest(t *testing.T, test quantumFsTest) {
	t.Parallel()

	testPc, _, _, _ := runtime.Caller(1)
	testName := runtime.FuncForPC(testPc).Name()
	th := &testHelper{
		t:          t,
		testName:   testName,
		testResult: make(chan string),
		testOutput: make([]string, 0, 1000),
	}

	defer th.endTest()

	// Allow tests to run for up to 1 seconds before considering them timed out
	go th.execute(test)

	var testResult string

	select {
	case <-time.After(1 * time.Second):
		testResult = "TIMED OUT"

	case testResult = <-th.testResult:
	}

	if !th.shouldFail && testResult != "" {
		th.t.Fatal(testResult)
	} else if th.shouldFail && testResult == "" {
		th.t.Fatal("Test is expected to fail")
	}
}

func (th *testHelper) execute(test quantumFsTest) {
	// Catch any panics and covert them into test failures
	defer func(th *testHelper) {
		err := recover()
		trace := ""

		// If the test passed pass that fact back to runTest()
		if err == nil {
			err = ""
		} else {
			// Capture the stack trace of the failure
			trace = BytesToString(debug.Stack())
		}

		result := err.(string)
		if trace != "" {
			result += "\nStack Trace: " + trace
		}

		th.testResult <- result
	}(th)

	test(th)
}

func abortFuse(th *testHelper) {
	if th.fuseConnection == 0 {
		// Nothing to abort
		return
	}

	// Forcefully abort the filesystem so it can be unmounted
	th.t.Logf("Aborting FUSE connection %d", th.fuseConnection)
	path := fmt.Sprintf("%s/connections/%d/abort", fusectlPath,
		th.fuseConnection)
	abort, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		// We cannot abort so we won't terminate. We are
		// truly wedged.
		panic("Failed to abort FUSE connection (open)")
	}

	if _, err := abort.Write([]byte("1")); err != nil {
		panic("Failed to abort FUSE connection (write)")
	}

	abort.Close()
}

// endTest cleans up the testing environment after the test has finished
func (th *testHelper) endTest() {
	exception := recover()

	if th.api != nil {
		th.api.Close()
	}

	if th.server != nil {
		if exception != nil {
			abortFuse(th)
		}

		if err := th.server.Unmount(); err != nil {
			abortFuse(th)

			runtime.GC()

			if err := th.server.Unmount(); err != nil {
				th.t.Fatalf("Failed to unmount quantumfs instance "+
					"after aborting: %v", err)
			}
			th.t.Fatalf("Failed to unmount quantumfs instance: %v", err)
		}
	}

	if th.tempDir != "" {
		if err := os.RemoveAll(th.tempDir); err != nil {
			th.t.Fatalf("Failed to cleanup temporary mount point: %v",
				err)
		}
	}

	if exception != nil {
		th.t.Fatalf("Test failed with exception: %v", exception)
	}

	th.logscan()
}

// Check the test output for errors
func (th *testHelper) logscan() {
	errors := make([]string, 0, 10)

	for _, line := range th.testOutput {
		if strings.Contains(line, "PANIC") {
			errors = append(errors, line)
		}
	}

	if !th.shouldFailLogscan && len(errors) != 0 {
		for _, err := range errors {
			th.t.Logf("FATAL message logged: %s", err)
		}
		th.t.Fatal("Test FAILED due to FATAL messages")
	} else if th.shouldFailLogscan && len(errors) == 0 {
		th.t.Fatal("Test FAILED due to missing FATAL messages")
	}
}

// This helper is more of a namespacing mechanism than a coherent object
type testHelper struct {
	mutex             sync.Mutex // Protects a mishmash of the members
	t                 *testing.T
	testName          string
	qfs               *QuantumFs
	tempDir           string
	server            *fuse.Server
	fuseConnection    int
	api               *quantumfs.Api
	testResult        chan string
	testOutput        []string
	shouldFail        bool
	shouldFailLogscan bool
}

func (th *testHelper) defaultConfig() QuantumFsConfig {
	tempDir, err := ioutil.TempDir("", "quantumfsTest")
	if err != nil {
		th.t.Fatalf("Unable to create temporary mount point: %v", err)
	}

	th.tempDir = tempDir
	mountPath := tempDir + "/mnt"

	os.Mkdir(mountPath, 0777)
	th.t.Logf("[%s] Using mountpath %s", th.testName, mountPath)

	config := QuantumFsConfig{
		CachePath:        "",
		CacheSize:        1 * 1024 * 1024,
		CacheTimeSeconds: 1,
		CacheTimeNsecs:   0,
		MountPath:        mountPath,
		WorkspaceDB:      processlocal.NewWorkspaceDB(),
		DurableStore:     processlocal.NewDataStore(),
	}
	return config
}

func (th *testHelper) startDefaultQuantumFs() {
	config := th.defaultConfig()
	th.startQuantumFs(config)
}

// Return the fuse connection id for the filesystem mounted at the given path
func fuseConnection(mountPath string) int {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		panic("Failed opening mountinfo")
	}
	defer file.Close()

	mountinfo := bufio.NewReader(file)

	for {
		bline, _, err := mountinfo.ReadLine()
		if err != nil {
			panic("Failed to find mount")
		}

		line := string(bline)

		if strings.Contains(line, mountPath) {
			fields := strings.SplitN(line, " ", 5)
			dev := strings.Split(fields[2], ":")[1]
			devInt, err := strconv.Atoi(dev)
			if err != nil {
				panic("Failed to convert dev to integer")
			}
			return devInt
		}
	}
	panic("Mount not found")
}

// If the filesystem panics, abort it and unmount it to prevent the test binary from
// hanging.
func serveSafely(th *testHelper) {
	defer func(th *testHelper) {
		exception := recover()
		if exception != nil {
			if th.fuseConnection != 0 {
				abortFuse(th)
			}
			th.t.Fatalf("FUSE panic'd: %v", exception)
		}
	}(th)

	th.server.Serve()
}

func (th *testHelper) startQuantumFs(config QuantumFsConfig) {
	var mountOptions = fuse.MountOptions{
		AllowOther:    true,
		MaxBackground: 1024,
		MaxWrite:      quantumfs.MaxBlockSize,
		FsName:        "cluster",
		Name:          th.testName,
	}

	quantumfs := NewQuantumFs(config)
	th.qfs = quantumfs.(*QuantumFs)

	writer := func(format string, args ...interface{}) (int, error) {
		return th.log(format, args...)
	}
	th.qfs.c.Qlog.SetWriter(writer)
	th.qfs.c.Qlog.SetLogLevels("daemon/*,datastore/*,workspacesdb/*,test/*")

	server, err := fuse.NewServer(quantumfs, config.MountPath, &mountOptions)
	if err != nil {
		th.t.Fatalf("Failed to create quantumfs instance: %v", err)
	}

	th.fuseConnection = fuseConnection(config.MountPath)

	th.server = server
	go serveSafely(th)
}

func (th *testHelper) log(format string, args ...interface{}) (int, error) {
	output := fmt.Sprintf("["+th.testName+"] "+format, args...)

	th.mutex.Lock()
	th.testOutput = append(th.testOutput, output)
	th.mutex.Unlock()

	th.t.Log(output)

	return len(output), nil
}

func (th *testHelper) getApi() *quantumfs.Api {
	if th.api != nil {
		return th.api
	}

	th.api = quantumfs.NewApiWithPath(th.relPath(quantumfs.ApiPath))
	return th.api
}

// Make the given path relative to the mount root
func (th *testHelper) relPath(path string) string {
	return th.tempDir + "/mnt/" + path
}

// Retrieve a list of FileDescriptor from an Inode
func (th *testHelper) fileDescriptorFromInodeNum(inodeNum uint64) []*FileDescriptor {
	handles := make([]*FileDescriptor, 0)

	th.qfs.mapMutex.Lock()

	for _, file := range th.qfs.fileHandles {
		fh, ok := file.(*FileDescriptor)
		if !ok {
			continue
		}

		if fh.inodeNum == InodeId(inodeNum) {
			handles = append(handles, fh)
		}
	}

	th.qfs.mapMutex.Unlock()

	return handles
}

// Retrieve the rootId of the given workspace
func (th *testHelper) workspaceRootId(namespace string,
	workspace string) quantumfs.ObjectKey {

	return th.qfs.c.workspaceDB.Workspace(namespace, workspace)
}

// Global test request ID incremented for all the running tests
var requestId = uint64(1000000000)

// Produce a request specific ctx variable to use for quantumfs internal calls
func (th *testHelper) newCtx() *ctx {
	reqId := atomic.AddUint64(&requestId, 1)
	c := th.qfs.c.req(reqId)
	c.Ctx.Vlog(qlog.LogTest, "Allocating request %d to test %s", reqId,
		th.testName)
	return c
}

// assert the condition is true. If it is not true then fail the test with the given
// message
func (th *testHelper) assert(condition bool, format string, args ...interface{}) {
	if !condition {
		msg := fmt.Sprintf(format, args)
		panic(msg)
	}
}

type crashOnWrite struct {
	FileHandle
}

func (crash *crashOnWrite) Write(c *ctx, offset uint64, size uint32, flags uint32,
	buf []byte) (uint32, fuse.Status) {

	panic("Intentional crash")
}

// If a quantumfs test fails then it may leave the filesystem mount hanging around in
// a blocked state. testHelper needs to forcefully abort and umount these to keep the
// system functional. Test this forceful unmounting here.
func TestPanicFilesystemAbort_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.shouldFailLogscan = true

		test.startDefaultQuantumFs()
		api := test.getApi()

		// Introduce a panicing error into quantumfs
		for k, v := range test.qfs.fileHandles {
			test.qfs.fileHandles[k] = &crashOnWrite{FileHandle: v}
		}

		// panic Quantumfs
		api.Branch("_null/null", "test/crash")
	})
}

// If a test never returns from some event, such as an inifinite loop, the test
// should timeout and cleanup after itself.
func TestTimeout_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.startDefaultQuantumFs()

		test.shouldFail = true
		time.Sleep(60 * time.Second)

		// If we get here then the test library didn't time us out and we
		// sould fail this test.
		test.shouldFail = false
		test.assert(false, "Test didn't fail due to timeout")
	})
}

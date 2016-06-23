// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

// Test library

import "bufio"
import "errors"
import "flag"
import "fmt"
import "io/ioutil"
import "math/rand"
import "os"
import "runtime"
import "runtime/debug"
import "strings"
import "strconv"
import "sync"
import "sync/atomic"
import "testing"
import "time"

import "github.com/aristanetworks/quantumfs"
import "github.com/aristanetworks/quantumfs/processlocal"
import "github.com/aristanetworks/quantumfs/qlog"

import "github.com/hanwen/go-fuse/fuse"

const fusectlPath = "/sys/fs/fuse/"

type quantumFsTest func(test *testHelper)

// startTest is a helper which configures the testing environment
func runTest(t *testing.T, test quantumFsTest) {
	t.Parallel()

	testPc, _, _, _ := runtime.Caller(1)
	testName := runtime.FuncForPC(testPc).Name()
	lastSlash := strings.LastIndex(testName, "/")
	testName = testName[lastSlash+1:]
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
			trace = strings.SplitN(trace, "\n", 8)[7]
		}

		var result string
		switch err.(type) {
		default:
			result = fmt.Sprintf("Unknown panic type: %v", err)
		case string:
			result = err.(string)
		case error:
			result = err.(error).Error()
		}

		if trace != "" {
			result += "\nStack Trace:\n" + trace
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
			th.t.Logf("Failed with exception, forcefully unmounting")
			abortFuse(th)
		}

		if err := th.server.Unmount(); err != nil {
			abortFuse(th)

			runtime.GC()

			if err := th.server.Unmount(); err != nil {
				th.t.Fatalf("Failed to unmount quantumfs instance "+
					"after aborting: %v", err)
			}
			th.t.Fatalf("Failed to unmount quantumfs instance, are you"+
				" leaking a file descriptor?: %v", err)
		}
	}

	if th.tempDir != "" {
		th.waitToBeUnmounted()
		time.Sleep(1 * time.Second)

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

func (th *testHelper) waitToBeUnmounted() {
	for i := 0; i < 100; i++ {
		mounts, err := ioutil.ReadFile("/proc/self/mountinfo")
		if err == nil {
			mounts := BytesToString(mounts)
			if !strings.Contains(mounts, th.tempDir) {
				th.log("Waited %d times to unmount", i)
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	panic("Filesystem didn't unmount in time")
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
	th.tempDir = testRunDir + "/" + th.testName
	mountPath := th.tempDir + "/mnt"

	os.MkdirAll(mountPath, 0777)
	th.log("Using mountpath %s", mountPath)

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
	for i := 0; i < 10; i++ {
		file, err := os.Open("/proc/self/mountinfo")
		if err != nil {
			panic(fmt.Sprintf("Failed opening mountinfo: %v", err))
		}
		defer file.Close()

		mountinfo := bufio.NewReader(file)

		for {
			bline, _, err := mountinfo.ReadLine()
			if err != nil {
				continue
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

		time.Sleep(50 * time.Millisecond)
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
	th.qfs.c.Qlog.SetLogLevels("daemon/*,datastore/*,workspacedb/*,test/*")

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

	th.api = quantumfs.NewApiWithPath(th.absPath(quantumfs.ApiPath))
	return th.api
}

// Make the given path absolute to the mount root
func (th *testHelper) absPath(path string) string {
	return th.tempDir + "/mnt/" + path
}

// Make the given path relative to the mount root
func (th *testHelper) relPath(path string) string {
	return strings.TrimPrefix(path, th.tempDir+"/mnt/")
}

// Return a random namespace/workspace name of given length
func randomNamespaceName(size int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz" + "0123456789-." +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	result := ""
	for i := 0; i < size; i++ {
		result += string(chars[rand.Intn(len(chars))])
	}

	return result
}

func TestRandomNamespaceName_test(t *testing.T) {
	runTest(t, func(test *testHelper) {
		name1 := randomNamespaceName(8)
		name2 := randomNamespaceName(8)
		name3 := randomNamespaceName(10)

		test.assert(len(name1) == 8, "name1 wrong length: %d", len(name1))
		test.assert(name1 != name2, "name1 == name2: '%s'", name1)
		test.assert(len(name3) == 10, "name3 wrong length: %d", len(name1))
	})
}

func (th *testHelper) nullWorkspaceRel() string {
	return quantumfs.NullNamespaceName + "/" + quantumfs.NullWorkspaceName
}

func (th *testHelper) nullWorkspace() string {
	return th.absPath(th.nullWorkspaceRel())
}

// Create a new workspace to test within
//
// Returns the absolute path of the workspace
func (th *testHelper) newWorkspace() string {
	api := th.getApi()

	src := th.nullWorkspaceRel()
	dst := randomNamespaceName(8) + "/" + randomNamespaceName(10)

	err := api.Branch(src, dst)
	th.assert(err == nil, "Failed to branch workspace: %v", err)

	return th.absPath(dst)
}

// Branch existing workspace into new random name
//
// Returns the relative path of the new workspace.
func (th *testHelper) branchWorkspace(original string) string {
	src := th.relPath(original)
	dst := randomNamespaceName(8) + "/" + randomNamespaceName(10)

	api := th.getApi()
	err := api.Branch(src, dst)

	th.assert(err == nil, "Failed to branch workspace: %s -> %s: %v", src, dst,
		err)

	return dst
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

	return th.qfs.c.workspaceDB.Workspace(&th.newCtx().Ctx, namespace, workspace)
}

// Global test request ID incremented for all the running tests
var requestId = uint64(1000000000)

// Temporary directory for this test run
var testRunDir string

func init() {
	var err error
	for i := 0; i < 10; i++ {
		testRunDir, err = ioutil.TempDir("", "quantumfsTest")
		if err != nil {
			continue
		}
		return
	}
	panic(fmt.Sprintf("Unable to create temporary test directory: %v", err))
}

func TestMain(m *testing.M) {
	flag.Parse()

	// Disable Garbage Collection. Because the tests provide both the filesystem
	// and the code accessing that filesystem the program is reentrant in ways
	// opaque to the golang scheduler. Thus we can end up in a deadlock situation
	// between two threads:
	//
	// ThreadFS is the filesystem, ThreadT is the test
	//
	//   ThreadFS                    ThreadT
	//                               Start filesystem syscall
	//   Start executing response
	//   <GC Wait>                   <Queue GC wait after syscal return>
	//                        DEADLOCK
	//
	// Because the filesystem request is blocked waiting on GC and the syscall
	// will never return to allow GC to progress, the test program is deadlocked.
	debug.SetGCPercent(-1)

	result := m.Run()

	os.RemoveAll(testRunDir)
	os.Exit(result)
}

// Produce a request specific ctx variable to use for quantumfs internal calls
func (th *testHelper) newCtx() *ctx {
	reqId := atomic.AddUint64(&requestId, 1)
	c := th.qfs.c.dummyReq(reqId)
	c.Ctx.Vlog(qlog.LogTest, "Allocating request %d to test %s", reqId,
		th.testName)
	return c
}

//only to be used for some testing - not all functions will work with this
func (c *ctx) dummyReq(request uint64) *ctx {
	requestCtx := &ctx{
		Ctx: quantumfs.Ctx{
			Qlog:      c.Qlog,
			RequestId: request,
		},
		qfs:          c.qfs,
		config:       c.config,
		workspaceDB:  c.workspaceDB,
		durableStore: c.durableStore,
		fuseCtx:      nil,
	}
	return requestCtx
}

// assert the condition is true. If it is not true then fail the test with the given
// message
func (th *testHelper) assert(condition bool, format string, args ...interface{}) {
	if !condition {
		msg := fmt.Sprintf(format, args...)
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
		test.qfs.mapMutex.Lock()
		for k, v := range test.qfs.fileHandles {
			test.qfs.fileHandles[k] = &crashOnWrite{FileHandle: v}
		}
		test.qfs.mapMutex.Unlock()

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

func printToFile(filename string, data string) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_RDWR,
		0777)
	if file == nil || err != nil {
		return errors.New("Unable to open file for RDRW")
	}

	written := 0
	for written < len(data) {
		var writeIt int
		writeIt, err = file.Write([]byte(data[written:]))
		written += writeIt
		if err != nil {
			return errors.New("Unable to write all data")
		}
	}
	file.Close()

	return nil
}

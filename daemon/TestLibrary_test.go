// Copyright (c) 2017 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

package daemon

import "bytes"
import "flag"
import "runtime/debug"
import "runtime"
import "testing"
import "fmt"
import "reflect"
import "strconv"
import "strings"
import "sync"
import "sync/atomic"
import "syscall"
import "os"
import "time"

import "github.com/aristanetworks/quantumfs"
import "github.com/aristanetworks/quantumfs/processlocal"
import "github.com/aristanetworks/quantumfs/qlog"
import "github.com/aristanetworks/quantumfs/testutils"
import "github.com/aristanetworks/quantumfs/thirdparty_backends"
import "github.com/aristanetworks/quantumfs/utils"

func TestMain(m *testing.M) {
	flag.Parse()

	// Precompute a bunch of our genData to save time during tests
	genData(40 * 1024 * 1024)

	PreTestRuns()
	result := m.Run()
	PostTestRuns()

	os.Exit(result)
}

func TestRandomNamespaceName(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		name1 := randomNamespaceName(8)
		name2 := randomNamespaceName(8)
		name3 := randomNamespaceName(10)

		test.Assert(len(name1) == 8, "name1 wrong length: %d", len(name1))
		test.Assert(name1 != name2, "name1 == name2: '%s'", name1)
		test.Assert(len(name3) == 10, "name3 wrong length: %d", len(name1))
	})
}

// If a quantumfs test fails then it may leave the filesystem mount hanging around in
// a blocked state. testHelper needs to forcefully abort and umount these to keep the
// system functional. Test this forceful unmounting here.
func TestPanicFilesystemAbort(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.ShouldFailLogscan = true

		api := test.getApi()

		// Introduce a panicing error into quantumfs
		test.qfs.mapMutex.Lock()
		for k, v := range test.qfs.fileHandles {
			test.qfs.fileHandles[k] = &crashOnWrite{FileHandle: v}
		}
		test.qfs.mapMutex.Unlock()

		// panic Quantumfs
		api.Branch("_null/_null/null", "branch/test/crash")
	})
}

// If a test never returns from some event, such as an inifinite loop, the test
// should timeout and cleanup after itself.
func TestTimeout(t *testing.T) {
	runTest(t, func(test *testHelper) {
		test.ShouldFail = true
		time.Sleep(60 * time.Second)

		// If we get here then the test library didn't time us out and we
		// sould fail this test.
		test.ShouldFail = false
		test.Assert(false, "Test didn't fail due to timeout")
	})
}

func TestGenData(t *testing.T) {
	runTestNoQfs(t, func(test *testHelper) {
		hardcoded := "012345678910111213141516171819202122232425262"
		data := genData(len(hardcoded))

		test.Assert(bytes.Equal([]byte(hardcoded), data),
			"Data gen function off: %s vs %s", hardcoded, data)
	})
}

type quantumFsTest func(test *testHelper)

// This is the normal way to run tests in the most time efficient manner
func runTest(t *testing.T, test quantumFsTest) {
	t.Parallel()
	runTestCommon(t, test, true, nil)
}

// If you need to initialize the QuantumFS instance in some special way,
// then use this variant.
func runTestNoQfs(t *testing.T, test quantumFsTest) {
	t.Parallel()
	runTestCommon(t, test, false, nil)
}

// configModifier is a function which is given the default configuration and should
// make whichever modifications the test requires in place.
type configModifierFunc func(test *testHelper, config *QuantumFsConfig)

// If you need to initialize QuantumFS with a special configuration, but not poke
// into its internals before the test proper begins, use this.
func runTestCustomConfig(t *testing.T, configModifier configModifierFunc,
	test quantumFsTest) {

	t.Parallel()
	runTestCommon(t, test, true, configModifier)
}

// If you need to initialize the QuantumFS instance in some special way and the test
// is relatively expensive, then use this variant.
func runTestNoQfsExpensiveTest(t *testing.T, test quantumFsTest) {
	runTestCommon(t, test, false, nil)
}

// If you have a test which is expensive in terms of CPU time, then use
// runExpensiveTest() which will not run it at the same time as other tests. This is
// to prevent multiple expensive tests from running concurrently and causing each
// other to time out due to CPU starvation.
func runExpensiveTest(t *testing.T, test quantumFsTest) {
	runTestCommon(t, test, true, nil)
}

func runTestCommon(t *testing.T, test quantumFsTest, startDefaultQfs bool,
	configModifier configModifierFunc) {

	// Since we grab the test name from the backtrace, it must always be an
	// identical number of frames back to the name of the test. Otherwise
	// multiple tests will end up using the same temporary directory and nothing
	// will work.
	//
	// 2 <testname>
	// 1 runTest/runExpensiveTest
	// 0 runTestCommon
	testPc, _, _, _ := runtime.Caller(2)
	testName := runtime.FuncForPC(testPc).Name()
	lastSlash := strings.LastIndex(testName, "/")
	testName = testName[lastSlash+1:]
	cachePath := TestRunDir + "/" + testName

	th := &testHelper{
		TestHelper: TestHelper{
			TestHelper: testutils.TestHelper{
				T:          t,
				TestName:   testName,
				TestResult: make(chan string, 2), // must be buffered
				StartTime:  time.Now(),
				CachePath:  cachePath,
				Logger: qlog.NewQlogExt(cachePath+"/ramfs",
					60*10000*24, NoStdOut),
			},
		},
	}

	th.CreateTestDirs()
	defer th.EndTest()

	// Allow tests to run for up to 1 seconds before considering them timed out.
	// If we are going to start a standard QuantumFS instance we can start the
	// timer before the test proper and therefore avoid false positive test
	// failures due to timeouts caused by system slowness as we try to mount
	// dozens of FUSE filesystems at once.
	if startDefaultQfs {
		config := th.defaultConfig()
		if configModifier != nil {
			configModifier(th, &config)
		}

		th.startQuantumFs(config)
	}

	th.Log("Finished test preamble, starting test proper")
	beforeTest := time.Now()
	go th.execute(test)

	testResult := th.WaitForResult()

	// Record how long the test took so we can make a histogram
	afterTest := time.Now()
	testutils.TimeMutex.Lock()
	testutils.TimeBuckets = append(testutils.TimeBuckets,
		testutils.TimeData{
			Duration: afterTest.Sub(beforeTest),
			TestName: testName,
		})
	testutils.TimeMutex.Unlock()

	if !th.ShouldFail && testResult != "" {
		th.Log("ERROR: Test failed unexpectedly:\n%s\n", testResult)
	} else if th.ShouldFail && testResult == "" {
		th.Log("ERROR: Test is expected to fail, but didn't")
	}
}

// execute the quantumfs test.
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
			trace = utils.BytesToString(debug.Stack())
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

		// This can hang if the channel isn't buffered because in some rare
		// situations the other side isn't there to read from the channel
		th.TestResult <- result
	}(th)

	test(th)
}

var genDataMutex sync.RWMutex
var precompGenData []byte
var genDataLast int

func genData(maxLen int) []byte {
	if maxLen > len(precompGenData) {
		// we need to expand the array
		genDataMutex.Lock()

		for len(precompGenData) <= maxLen {
			precompGenData = append(precompGenData,
				strconv.Itoa(genDataLast)...)
			genDataLast++
		}

		genDataMutex.Unlock()
	}
	genDataMutex.RLock()
	defer genDataMutex.RUnlock()

	return precompGenData[:maxLen]
}

// testHelper holds the variables important to maintain the state of testing
// in a package. This helper is more of a namespacing mechanism than a
// coherent object.
type testHelper struct {
	TestHelper
}

// Retrieve a list of FileDescriptor from an Inode
func (th *testHelper) fileDescriptorFromInodeNum(inodeNum uint64) []*FileDescriptor {
	handles := make([]*FileDescriptor, 0)

	defer th.qfs.mapMutex.Lock().Unlock()

	for _, file := range th.qfs.fileHandles {
		fh, ok := file.(*FileDescriptor)
		if !ok {
			continue
		}

		if fh.inodeNum == InodeId(inodeNum) {
			handles = append(handles, fh)
		}
	}

	return handles
}

// Return the inode number from QuantumFS. Fails if the absolute path doesn't exist.
func (th *testHelper) getInodeNum(path string) InodeId {
	var stat syscall.Stat_t
	err := syscall.Stat(path, &stat)
	th.Assert(err == nil, "Error grabbing file inode (%s): %v", path, err)

	return InodeId(stat.Ino)
}

// Retrieve the Inode from Quantumfs. Returns nil is not instantiated
func (th *testHelper) getInode(path string) Inode {
	inodeNum := th.getInodeNum(path)
	return th.qfs.inodeNoInstantiate(&th.qfs.c, inodeNum)
}

// Retrieve the rootId of the given workspace
func (th *testHelper) workspaceRootId(typespace string, namespace string,
	workspace string) quantumfs.ObjectKey {

	key, err := th.qfs.c.workspaceDB.Workspace(&th.newCtx().Ctx,
		typespace, namespace, workspace)
	th.Assert(err == nil, "Error fetching key")

	return key
}

// Produce a request specific ctx variable to use for quantumfs internal calls
func (th *testHelper) newCtx() *ctx {
	reqId := atomic.AddUint64(&requestId, 1)
	c := th.qfs.c.dummyReq(reqId)
	c.Ctx.Vlog(qlog.LogTest, "Allocating request %d to test %s", reqId,
		th.TestName)
	return c
}

func (th *testHelper) remountFilesystem() {
	th.Log("Remounting filesystem")
	err := syscall.Mount("", th.TempDir+"/mnt", "", syscall.MS_REMOUNT, "")
	th.Assert(err == nil, "Unable to force vfs to drop dentry cache: %v", err)
}

// Modify the QuantumFS cache time to 100 milliseconds
func cacheTimeout100Ms(test *testHelper, config *QuantumFsConfig) {
	config.CacheTimeSeconds = 0
	config.CacheTimeNsecs = 100000
}

// Modify the QuantumFS flush delay to 100 milliseconds
func dirtyDelay100Ms(test *testHelper, config *QuantumFsConfig) {
	config.DirtyFlushDelay = 100 * time.Millisecond
}

// Extract namespace and workspace path from the absolute path of
// a workspaceroot
func (th *testHelper) getWorkspaceComponents(abspath string) (string,
	string, string) {

	relpath := th.relPath(abspath)
	components := strings.Split(relpath, "/")

	return components[0], components[1], components[2]
}

// Convert an absolute workspace path to the matching WorkspaceRoot object
func (th *testHelper) getWorkspaceRoot(workspace string) *WorkspaceRoot {
	parts := strings.Split(th.relPath(workspace), "/")
	wsr, ok := th.qfs.getWorkspaceRoot(&th.qfs.c,
		parts[0], parts[1], parts[2])

	th.Assert(ok, "WorkspaceRoot object for %s not found", workspace)

	return wsr
}

func (th *testHelper) getAccessList(workspace string) map[string]bool {
	return th.getWorkspaceRoot(workspace).getList()
}

func (th *testHelper) AssertAccessList(testlist map[string]bool,
	wsrlist map[string]bool, message string) {

	eq := reflect.DeepEqual(testlist, wsrlist)
	msg := fmt.Sprintf("\ntestlist:%v\n, wsrlist:%v\n", testlist, wsrlist)
	message = message + msg
	th.Assert(eq, message)
}

func (th *testHelper) etherFilesystemConfig() QuantumFsConfig {
	mountPath := th.TempDir + "/mnt"

	datastorePath := th.TempDir + "/ether"
	datastore := thirdparty_backends.NewEtherFilesystemStore(datastorePath)

	config := QuantumFsConfig{
		CachePath:        th.TempDir + "/ramfs",
		CacheSize:        1 * 1024 * 1024,
		CacheTimeSeconds: 1,
		CacheTimeNsecs:   0,
		DirtyFlushDelay:  30 * time.Second,
		MountPath:        mountPath,
		WorkspaceDB:      processlocal.NewWorkspaceDB(""),
		DurableStore:     datastore,
	}
	return config
}

// Copyright (c) 2017 Arista Networks, Inc.
// Use of this source code is governed by the Apache License 2.0
// that can be found in the COPYING file.

#ifndef QFSCLIENT_QFS_CLIENT_IMPLEMENTATION_H_
#define QFSCLIENT_QFS_CLIENT_IMPLEMENTATION_H_

#include "QFSClient/qfs_client.h"

#include <stdint.h>
#include <sys/types.h>

#include <gtest/gtest_prod.h>
#include <jansson.h>

#include <string>
#include <unordered_map>
#include <vector>

namespace qfsclient {

const char kApiPath[] = "api";
const int kInodeIdApi = 2;

// Class used for holding internal context about an in-flight API call. It may be
// passed between functions used to handle an API call and should should be created
// on the stack so that useful cleanup happens automatically.
class ApiContext {
 public:
	ApiContext();
	~ApiContext();

	void SetRequestJsonObject(json_t *request_json_object);
	json_t *GetRequestJsonObject() const;

	void SetResponseJsonObject(json_t *response_json_object);
	json_t *GetResponseJsonObject() const;

 private:
	json_t *request_json_object;
	json_t *response_json_object;
};

// forward declarations
class CommandBuffer;
class TestHook;

// ApiImpl provides the concrete implentation for QuantumFS API calls and whatever
// related support logic they need. If an ApiImpl object is constructed with no
// path, it will start looking for the API file in the current working directory
// and work upwards towards the root from there. If it is constructed with a path,
// then it is assumed that the API file will be found at the given location.
class ApiImpl: public Api {
 public:
	ApiImpl();
	explicit ApiImpl(const char *path);
	virtual ~ApiImpl();

	// Attempts to open the api file - including attempting to determine
	// its location if the Api object was constructed without being given
	// a path to the location of the api file. Returns an error object to
	// indicate the outcome.
	Error Open();

	// Open the Api file without DIRECT_IO. Not for use with real quantumfs
	Error TestOpen();

	// Closes the api file if it's still open.
	void Close();

	// implemented API functions
	virtual Error GetAccessed(const char *workspace_root, PathsAccessed *paths);

	virtual Error InsertInode(const char *destination,
				  const char *key,
				  uint32_t permissions,
				  uint32_t uid,
				  uint32_t gid);

	virtual Error Branch(const char *source, const char *destination);

	virtual Error Delete(const char *workspace);

	virtual Error SetBlock(const std::vector<byte> &key,
			       const std::vector<byte> &data);

	virtual Error GetBlock(const std::vector<byte> &key,
			       std::vector<byte> *data);

	// The libqfs method for finding the api will not recognize our hacked test
	// api as being real, since it isn't a real api file, so we need to use our
	// own method for finding the api file in tests.
	Error DeterminePathInTest();

 private:
	// Open an Api
	Error OpenCommon(bool directIo);

	// Work out the location of the api file (which must be called 'api'
	// and have an inode ID of 2) by looking in the current directory
	// and walking up the directory tree towards the root until it's found.
	// Returns an error object to indicate the outcome.
	Error DeterminePath();

	// Writes the given command to the api file and immediately tries to
	// read a response form the same file. Returns an error object to
	// indicate the outcome.
	Error SendCommand(const CommandBuffer &command, CommandBuffer *response);

	// Writes the given command to the api file. Returns an error object to
	// indicate the outcome.
	Error WriteCommand(const CommandBuffer &command);

	// Attempts to read a response from the api file. Returns an error object to
	// indicate the outcome.
	Error ReadResponse(CommandBuffer *command);

	// Given a workspace name, test it for validity, returning an error to
	// indicate the name's validity.
	Error CheckWorkspaceNameValid(const char *workspace_name);

	// Given a workspace path, test it for validity, returning an error to
	// indicate the path's validity.
	Error CheckWorkspacePathValid(const char *workspace_path);

	int fd;

	// We use the presence of a value in this member variable to indicate that
	// the API file's location is known (either because it was passed to the
	// Api constructor, or because it was found by DeterminePath()). It doesn't
	// necessarily mean that the file has been opened: Api::Open() should do
	// that. Api::Open() should still be called before trying to call an API
	// function.
	std::string path;

	// Expected inode ID of the api file. The only reason we have this
	// instead of using the INODE_ID_API constant is that the unit tests
	// need to modify it (so that they can test against an arbitrary
	// temporary file which won't have an inode ID that's known in
	// advance)
	ino_t api_inode_id;

	// Pointer to a TestHook instance (used for testing ONLY). The
	// purpose of the TestHook class is described along with its
	// definition.
	TestHook *test_hook;

	// Internal member function to perform processing common to all API calls,
	// such as parsing JSON and checking for response errors
	Error CheckCommonApiResponse(const CommandBuffer &response,
				     ApiContext *context);

	// Send the JSON representation of the command to the API file and parse the
	// response, then check the response for an error. The context object will
	// be used to carry the request JSON object so that it gets released
	// properly and the parsed JSON response object for use by the next stage.
	Error SendJson(ApiContext *context);

	// Convert the JSON response received for the GetAccessed() API call into
	// a structure ready for formatting and then writing to stdout. Returns
	// an Error struct to indicate success or otherwise
	Error PrepareAccessedListResponse(
		const ApiContext *context,
		PathsAccessed *accessed_list);

	friend class QfsClientTest;
	FRIEND_TEST(QfsClientTest, SendCommandTest);
	FRIEND_TEST(QfsClientTest, SendLargeCommandTest);
	FRIEND_TEST(QfsClientTest, SendCommandFileRemovedTest);
	FRIEND_TEST(QfsClientTest, SendCommandNoFileTest);
	FRIEND_TEST(QfsClientTest, SendCommandCantOpenFileTest);
	FRIEND_TEST(QfsClientTest, WriteCommandFileNotOpenTest);
	FRIEND_TEST(QfsClientTest, OpenTest);
	FRIEND_TEST(QfsClientTest, CheckWorkspaceNameValidTest);
	FRIEND_TEST(QfsClientTest, CheckWorkspacePathValidTest);

	friend class QfsClientApiTest;
	FRIEND_TEST(QfsClientApiTest, CheckCommonApiResponseTest);
	FRIEND_TEST(QfsClientApiTest, CheckCommonApiResponseBadJsonTest);
	FRIEND_TEST(QfsClientApiTest, CheckCommonApiMissingJsonObjectTest);
	FRIEND_TEST(QfsClientApiTest, PrepareAccessedListResponseTest);
	FRIEND_TEST(QfsClientApiTest, PrepareAccessedListResponseNoAccessListTest);
	FRIEND_TEST(QfsClientApiTest, SendJsonTest);
	FRIEND_TEST(QfsClientApiTest, SendJsonTestJsonTooBig);

	FRIEND_TEST(QfsClientDeterminePathTest, DeterminePathTest);

	FRIEND_TEST(QfsClientCommandBufferTest, FreshBufferTest);
	FRIEND_TEST(QfsClientCommandBufferTest, ResetTest);
	FRIEND_TEST(QfsClientCommandBufferTest, AppendTest);
	FRIEND_TEST(QfsClientCommandBufferTest, AppendAndCopyLotsTest);
	FRIEND_TEST(QfsClientCommandBufferTest, CopyStringTest);
};

// CommandBuffer is used internally to store the raw content of a command to
// send to (or a response received from) the API - typically in JSON format.
class CommandBuffer {
 public:
	CommandBuffer();
	virtual ~CommandBuffer();

	// Remove the trailing zeros from the tail of the response
	void Sanitize();

	// Copy the contents of the given CommandBuffer into this one
	void Copy(const CommandBuffer &source);

	// Return a const pointer to the data in the buffer
	const byte *Data() const;

	// Return the size of the data stored in the buffer
	size_t Size() const;

	// Reset the buffer such that it will contain no data and will
	// have a zero size
	void Reset();

	// Append a block of data to the buffer. Returns an error if the
	// buffer would have to be grown too large to add this block
	ErrorCode Append(const byte *data, size_t size);

	// Copy a string into the buffer. An error will be returned if
	// the buffer would have to be grown too large to fit the string.
	ErrorCode CopyString(const char *s);

 private:
	std::vector<byte> data;

	FRIEND_TEST(QfsClientApiTest, CheckCommonApiResponseBadJsonTest);
	FRIEND_TEST(QfsClientApiTest, CheckCommonApiMissingJsonObjectTest);

	FRIEND_TEST(QfsClientCommandBufferTest, AppendTest);
	FRIEND_TEST(QfsClientCommandBufferTest, AppendAndCopyLotsTest);
	FRIEND_TEST(QfsClientCommandBufferTest, CopyStringTest);
};

// Class to be implemented by tests ONLY that a test can supply; if an instance
// of this class is supplied, then its PostWriteHook() method will be called
// by SendCommand() after writing a command, and PreReadHook() will be called
// before reading the response. This allows a test to check exactly what got
// written to the api file by WriteCommand() and to place a test response for use
// by ReadCommand() instead of having ReadCommand() read from the API file.
class TestHook {
 public:
	virtual Error PostWriteHook() = 0;
	virtual Error PreReadHook(CommandBuffer *read_result) = 0;
};

}  // namespace qfsclient

#endif  // QFSCLIENT_QFS_CLIENT_IMPLEMENTATION_H_


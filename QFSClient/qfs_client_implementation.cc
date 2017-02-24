// Copyright (c) 2016 Arista Networks, Inc.  All rights reserved.
// Arista Networks, Inc. Confidential and Proprietary.

#include "qfs_client_implementation.h"

#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include <algorithm>
#include <ios>
#include <vector>

#include <jansson.h>

#include "qfs_client.h"
#include "qfs_client_data.h"
#include "qfs_client_test.h"
#include "qfs_client_util.h"

namespace qfsclient {

ApiContext::ApiContext() {

};

ApiContext::~ApiContext() {

}

ApiImpl::CommandBuffer::CommandBuffer() {

}

ApiImpl::CommandBuffer::~CommandBuffer() {

}

// Return a const pointer to the data in the buffer
const byte *ApiImpl::CommandBuffer::Data() const {
	return this->data.data();
}

// Return the size of the data stored in the buffer
size_t ApiImpl::CommandBuffer::Size() const {
	return this->data.size();
}

// Reset the buffer such that it will contain no data and will
// have a zero size
void ApiImpl::CommandBuffer::Reset() {
	this->data.clear();
}

// Append a block of data to the buffer. Returns an error if the
// buffer would have to be grown too large to add this block
ErrorCode ApiImpl::CommandBuffer::Append(const byte *data, size_t size) {
	try {
		this->data.insert(this->data.end(), data, data + size);
	}
	catch (...) {
		return kBufferTooBig;
	}

	return kSuccess;
}

// copy a string into the buffer. An error will be returned if
// the buffer would have to be grown too large to fit the string.
ErrorCode ApiImpl::CommandBuffer::CopyString(const char *s) {
	this->data.clear();

	return this->Append((const byte *)s, 1 + strlen(s));
}

Error GetApi(Api **api) {
	*api = new ApiImpl();
	return util::getError(kSuccess);
}

Error GetApi(const char *path, Api **api) {
	*api = new ApiImpl(path);
	return util::getError(kSuccess);
}

void ReleaseApi(Api *api) {
	if (api != NULL) {
		delete (ApiImpl*)api;
	}
}

ApiImpl::ApiImpl()
	: path(""),
	  api_inode_id(kInodeIdApi),
	  send_test_hook(NULL) {
}

ApiImpl::ApiImpl(const char *path)
	: path(path),
	  api_inode_id(kInodeIdApi),
	  send_test_hook(NULL) {
}

ApiImpl::~ApiImpl() {
	Close();
}

Error ApiImpl::Open() {
	if (this->path.length() == 0) {
		// Path was not passed to constructor: determine path
		Error err = this->DeterminePath();
		if (err.code != kSuccess) {
			return err;
		}
	}

	if (!this->file.is_open()) {
		this->file.open(this->path.c_str(),
				std::ios::in | std::ios::out | std::ios::binary);

		if (this->file.fail()) {
			return util::getError(kCantOpenApiFile, this->path);
		}
	}

	return util::getError(kSuccess);
}

void ApiImpl::Close() {
	if (this->file.is_open()) {
		this->file.close();
	}
}

Error ApiImpl::SendCommand(const CommandBuffer &command, CommandBuffer *response) {
	Error err = this->Open();
	if (err.code != kSuccess) {
		return err;
	}

	err = this->WriteCommand(command);
	if (err.code != kSuccess) {
		return err;
	}

	if (this->send_test_hook) {
		err = this->send_test_hook->SendTestHook();
		if (err.code != kSuccess) {
			return err;
		}
	}

	return this->ReadResponse(response);
}

Error ApiImpl::WriteCommand(const CommandBuffer &command) {
	if (!this->file.is_open()) {
		return util::getError(kApiFileNotOpen);
	}

	this->file.seekp(0);
	if (this->file.fail()) {
		return util::getError(kApiFileSeekFail, this->path);
	}

	this->file.write((const char *)command.Data(), command.Size());
	if (this->file.fail()) {
		return util::getError(kApiFileWriteFail, this->path);
	}

	this->file.flush();
	if (this->file.fail()) {
		return util::getError(kApiFileFlushFail, this->path);
	}

	return util::getError(kSuccess);
}

Error ApiImpl::ReadResponse(CommandBuffer *command) {
	ErrorCode err = kSuccess;

	if (!this->file.is_open()) {
		return util::getError(kApiFileNotOpen);
	}

	this->file.seekg(0);
	if (this->file.fail()) {
		return util::getError(kApiFileSeekFail);
	}

	// read up to 4k at a time, stopping on EOF
	command->Reset();

	byte data[4096];
	while(!this->file.eof()) {
		this->file.read((char *)data, sizeof(data));

		if (this->file.fail() && !(this->file.eof())) {
			// any read failure *except* an EOF is a failure
			return util::getError(kApiFileReadFail, this->path);
		}

		size_t size = this->file.gcount();
		err = command->Append(data, size);

		if (err != kSuccess) {
			return util::getError(err);
		}
	}

	// clear the file stream's state, because it will remain in an error state
	// after hitting an EOF
	this->file.clear();

	return util::getError(err);
}

Error ApiImpl::DeterminePath() {
	// getcwd() with a NULL first parameter results in a buffer of whatever size
	// is required being allocated, which we must then free. PATH_MAX isn't
	// known at compile time (and it is possible for paths to be longer than
	// PATH_MAX anyway)
	char *cwd = getcwd(NULL, 0);
	if (!cwd) {
		return util::getError(kDontKnowCwd);
	}

	std::vector<std::string> directories;
	std::string path;
	std::string currentDir(cwd);

	free(cwd);

	util::Split(currentDir, "/", &directories);

	while (true) {
		util::Join(directories, "/", &path);
		path = "/" + path + "/" + kApiPath;

		struct stat path_status;

		if (lstat(path.c_str(), &path_status) == 0) {
			if (((S_ISREG(path_status.st_mode)) ||
			     (S_ISLNK(path_status.st_mode))) &&
			    (path_status.st_ino == this->api_inode_id)) {
				// we found an API *file* with the correct
				// inode ID: success
				this->path = path;
				return util::getError(kSuccess, this->path);
			}

			// Note: it's valid to have a file *or* directory called
			// 'api' that isn't the actual api file: in that case we
			// should just keep walking up the tree towards the root
		}

		if (directories.size() == 0) {
			// We got to / without finding the api file: fail
			return util::getError(kCantFindApiFile, currentDir);
		}

		// Remove last entry from directories and continue moving up
		// the directory tree by one level
		directories.pop_back();
	}

	return util::getError(kCantFindApiFile, currentDir);
}

Error ApiImpl::CheckWorkspacePathValid(const char *workspace_root) {
	std::string str(workspace_root);

	// path must have TWO '/' characters...
	size_t first = str.find('/');
	if (first == std::string::npos) {
		return util::getError(kWorkspacePathInvalid, workspace_root);
	}

	// ...but no more than two
	size_t second = str.find('/', first + 1);
	if (second == std::string::npos) {
		return util::getError(kWorkspacePathInvalid, workspace_root);
	}
	size_t third = str.find('/', second + 1);
	if (third != std::string::npos) {
		return util::getError(kWorkspacePathInvalid, workspace_root);
	}

	return util::getError(kSuccess);
}

Error ApiImpl::CheckCommonApiResponse(const CommandBuffer &response,
				      ApiContext *context) {
	json_error_t json_error;

	// parse JSON in response into a std::unordered_map<std::string, bool>
	json_t *response_json = json_loads((const char *)response.Data(),
					   response.Size(),
					   &json_error);

	((util::JsonApiContext*)context)->SetJsonObject(response_json);

	if (response_json == NULL) {
		std::string details = util::buildJsonErrorDetails(
			json_error.text, (const char *)response.Data());
		return util::getError(kJsonDecodingError, details);
	}

	json_t *response_json_obj;
	// note about cleaning up response_json_obj: this isn't necessary
	// as long as we use format character 'o' below, because in this case
	// the reference count of response_json isn't increased and we don't
	// leak any references. However, format character 'O' *will* increase the
	// reference count and in that case we would need to clean up afterwards.
	int success = json_unpack_ex(response_json, &json_error, 0,
				     "o", &response_json_obj);

	if (success != 0) {
		std::string details = util::buildJsonErrorDetails(
			json_error.text, (const char *)response.Data());
		return util::getError(kJsonDecodingError, details);
	}

	json_t *error_json_obj = json_object_get(response_json_obj, kErrorCode);
	if (error_json_obj == NULL) {
		std::string details = util::buildJsonErrorDetails(
			kErrorCode, (const char *)response.Data());
		return util::getError(kMissingJsonObject, details);
	}

	json_t *message_json_obj = json_object_get(response_json_obj, kMessage);
	if (message_json_obj == NULL) {
		std::string details = util::buildJsonErrorDetails(
			kMessage, (const char *)response.Data());
		return util::getError(kMissingJsonObject, details);
	}

	if (!json_is_integer(error_json_obj)) {
		std::string details = util::buildJsonErrorDetails(
			"error code in response JSON is not valid",
			(const char *)response.Data());
		return util::getError(kJsonDecodingError,
				      details);
	}

	CommandError apiError = (CommandError)json_integer_value(error_json_obj);
	if (apiError != kCmdOk) {
		std::string api_error = util::getApiError(
			apiError,
			json_string_value(message_json_obj));

		std::string details = util::buildJsonErrorDetails(
			api_error, (const char *)response.Data());

		return util::getError(kApiError, details);
	}

	return util::getError(kSuccess);
}

Error ApiImpl::SendJson(const void *request_json_ptr, ApiContext *context) {
	json_t *request_json = (json_t *)request_json_ptr;

	// we pass these flags to json_dumps() because:
	//	JSON_COMPACT: there's no good reason for verbose JSON
	//	JSON_SORT_KEYS: so that the tests can get predictable JSON and will
	//		be able to compare generated JSON reliably
	char *request_json_str = json_dumps(request_json,
					    JSON_COMPACT | JSON_SORT_KEYS);

	CommandBuffer command;

	command.CopyString(request_json_str);

	free(request_json_str);    // free the JSON string created by json_dumps()

	// send CommandBuffer and receive response in another one
	CommandBuffer response;
	Error err = this->SendCommand(command, &response);
	if (err.code != kSuccess) {
		 return err;
	}

	err = this->CheckCommonApiResponse(response, context);
	if (err.code != kSuccess) {
		 return err;
	}

	return util::getError(kSuccess);
}

Error ApiImpl::GetAccessed(const char *workspace_root) {
	Error err = this->CheckWorkspacePathValid(workspace_root);
	if (err.code != kSuccess) {
		return err;
	}

	// create JSON in a CommandBuffer with:
	//	CommandId = kGetAccessed and
	//	WorkspaceRoot = workspace_root
	// See http://jansson.readthedocs.io/en/2.4/apiref.html#building-values for
	// an explanation of the format strings that json_pack_ex can take.
	json_error_t json_error;
	json_t *request_json = json_pack_ex(&json_error, 0,
					    "{s:i,s:s}",
					    kCommandId, kCmdGetAccessed,
					    kWorkspaceRoot, workspace_root);
	if (request_json == NULL) {
		return util::getError(kJsonEncodingError, json_error.text);
	}

	util::JsonApiContext context;
	err = this->SendJson(request_json, &context);
	json_decref(request_json); // release the JSON object
	if (err.code != kSuccess) {
		return err;
	}

	std::unordered_map<std::string, bool> accessed;

	err = this->PrepareAccessedListResponse(&context, &accessed);
	if (err.code != kSuccess) {
		return err;
	}

	// call printAccessList using parsed response
	std::string formattedAccessedList = this->FormatAccessedList(accessed);
	printf("%s", formattedAccessedList.c_str());

	return util::getError(kSuccess);
}

Error ApiImpl::PrepareAccessedListResponse(
	const ApiContext *context,
	std::unordered_map<std::string, bool> *accessed_list) {

	json_error_t json_error;
	json_t *response_json = ((util::JsonApiContext*)context)->GetJsonObject();

	// if we get to this point, there was no error response; the object field
	// 'AccessList' is a Go JSON mapping of a Go map[string]bool - an
	// Object whose field names are the string keys in the map and whose values
	// are bools. A true value in the map means that the value's corresponding
	// key is the name of a file that has been created, whereas a false
	// value in the map means that the value's corresponding key is the name
	// of a file that has been accessed.
	json_t *accessed_list_json_obj = json_object_get(response_json,
							 kAccessList);
	if (accessed_list_json_obj == NULL) {
		return util::getError(kMissingJsonObject, "AccessList");
	}

	const char *k;
	json_t *v;
	json_object_foreach(accessed_list_json_obj, k, v) {
		// check that v is a bool
		if (json_is_boolean(v)) {
			// add value to map with key from AccessList
			(*accessed_list)[k] = json_is_true(v);
		}
	}

	return util::getError(kSuccess);
}

std::string ApiImpl::FormatAccessedList(
	const std::unordered_map<std::string, bool> &accessed) {

	std::string result = "------ Created Files ------\n";
	for (auto kv : accessed) {
		if (kv.second) {
			result += kv.first + "\n";
		}
	}

	result += "------ Accessed Files ------\n";
	for (auto kv : accessed) {
		if (!kv.second) {
			result += kv.first + "\n";
		}
	}

	return result;
}

} // namespace qfsclient

COMMANDS=quantumfsd qfs qparse emptykeys qupload qwalker qloggerdb
COMMANDS386=qfs-386 qparse-386
PKGS_TO_TEST=quantumfs quantumfs/daemon quantumfs/qlog
PKGS_TO_TEST+=quantumfs/thirdparty_backends quantumfs/systemlocal
PKGS_TO_TEST+=quantumfs/processlocal quantumfs/walker
PKGS_TO_TEST+=quantumfs/utils/aggregatedatastore
PKGS_TO_TEST+=quantumfs/utils/excludespec quantumfs/grpc
PKGS_TO_TEST+=quantumfs/grpc/server quantumfs/qlogstats
PKGS_TO_TEST+=quantumfs/cmd/qupload

# It's common practice to use a 'v' prefix on tags, but the prefix should be
# removed when making the RPM version string.
#
# Use "git describe" as the basic RPM version data.  If there are no tags
# yet, simulate a v0 tag on the initial/empty repo and a "git describe"-like
# tag (eg v0-12-gdeadbee) so there's a legitimate, upgradeable RPM version.
#
# Include "-dirty" on the end if there are any uncommitted changes.
#
# Replace hyphens with underscores; RPM uses them to separate version/release.
version := $(shell git describe --dirty --match "v[0-9]*" 2>/dev/null || echo "v0-`git rev-list --count HEAD`-g`git describe --dirty --always`")
RPM_VERSION := $(shell echo "$(version)" | sed -e "s/^v//" -e "s/-/_/g")
RPM_RELEASE := 1

.PHONY: all vet $(COMMANDS) $(COMMANDS386) $(PKGS_TO_TEST)

all: lockcheck cppstyle vet $(COMMANDS) $(COMMANDS386) $(PKGS_TO_TEST) wsdbservice qfsclient

clean:
	rm -f $(COMMANDS) $(COMMANDS386) quantumfsd-static *.rpm

fetch:
	go get -u google.golang.org/grpc
	go get -u github.com/golang/protobuf/protoc-gen-go
	for cmd in $(COMMANDS); do \
		echo "Fetching $$cmd"; \
		go get github.com/aristanetworks/quantumfs/cmd/$$cmd; \
	done

vet: $(PKGS_TO_TEST) $(COMMANDS)
	go vet -n ./... | while read -r line; do if  [[ ! "$$line" =~ .*encoding.* ]]; then eval $$line || exit 1; fi; done

lockcheck:
	./lockcheck.sh

cppstyle:
	./cpplint.py QFSClient/*.cc QFSClient/*.h

encoding/metadata.capnp.go: encoding/metadata.capnp
	@if which capnp &>/dev/null; then \
		cd encoding; capnp compile -ogo metadata.capnp; \
	else \
		echo "Error: capnp not found. If you didn't modify encoding/metadata.capnp try 'touch encoding/metadata.capnp.go' to fix the build."; \
		exit 1; \
	fi

grpc/rpc/rpc.pb.go: grpc/rpc/rpc.proto
	protoc -I grpc/rpc/ grpc/rpc/rpc.proto --go_out=plugins=grpc:grpc/rpc

$(COMMANDS): encoding/metadata.capnp.go
	go build -gcflags '-e' -ldflags "-X main.version=$(version)" github.com/aristanetworks/quantumfs/cmd/$@
	mkdir -p $(GOPATH)/bin
	cp -r $(GOPATH)/src/github.com/aristanetworks/quantumfs/$@ $(GOPATH)/bin/$@
	sudo -E go test github.com/aristanetworks/quantumfs/cmd/$@

quantumfsd-static: quantumfsd
	go build -gcflags '-e' -o quantumfsd-static -ldflags "-X main.version=$(version) -extldflags -static" github.com/aristanetworks/quantumfs/cmd/quantumfsd

$(COMMANDS386): encoding/metadata.capnp.go
	GOARCH=386 go build -gcflags '-e' -o $@ -ldflags "-X main.version=$(version)" github.com/aristanetworks/quantumfs/cmd/$(subst -386,,$@)

wsdbservice:
	go build -gcflags '-e' -o cmd/wsdbservice/wsdbservice -ldflags "-X main.version=$(version) -extldflags -static" github.com/aristanetworks/quantumfs/cmd/wsdbservice

dockerWsdb: wsdbservice
	cd cmd/wsdbservice; docker build -t registry.docker.sjc.aristanetworks.com:5000/qubit-tools/wsdbservice:$(version) .

uploadDocker: dockerWsdb
	cd cmd/wsdbservice; docker push registry.docker.sjc.aristanetworks.com:5000/qubit-tools/wsdbservice:$(version)

$(PKGS_TO_TEST): encoding/metadata.capnp.go grpc/rpc/rpc.pb.go
	sudo -E go test $(QFS_GO_TEST_ARGS) -gcflags '-e' github.com/aristanetworks/$@

rpm-ver:
	@echo "version='$(version)'"
	@echo "RPM version='$(RPM_VERSION)'"
	@echo "RPM release='$(RPM_RELEASE)'"

check-fpm:
	fpm --help &> /dev/null || \
	(echo "Installing fpm" && \
		sudo yum install -y gcc libffi-devel ruby-devel rubygems && \
		sudo gem install --no-ri --no-rdoc fpm \
	)

quploadRPM: check-fpm $(COMMANDS)
	fpm -f -s dir -t rpm -m 'quantumfs-dev@arista.com' -n QuantumFS-upload --no-depends \
		--license='Arista Proprietary' \
		--vendor='Arista Networks' \
		--url http://gut/repos/quantumfs \
		--description='A tool to upload directory hierarchy into datastore' \
		--version $(RPM_VERSION) --iteration $(RPM_RELEASE) \
		./qupload=/usr/bin/qupload

qfsRPM: check-fpm $(COMMANDS)
	fpm -f -s dir -t rpm -m 'quantumfs-dev@arista.com' -n QuantumFS --no-depends \
		--license='Arista Proprietary' \
		--vendor='Arista Networks' \
		--url http://gut/repos/quantumfs \
		--description='A distributed filesystem optimized for large scale software development' \
		--depends libstdc++ \
		--depends fuse \
		--after-install systemd_reload \
		--after-remove systemd_reload \
		--after-upgrade systemd_reload \
		--version $(RPM_VERSION) --iteration $(RPM_RELEASE) \
		./quantumfsd=/usr/sbin/quantumfsd \
		./qfs=/usr/bin/qfs \
		./qparse=/usr/sbin/qparse \
		./qloggerdb=/usr/sbin/qloggerdb \
		./qloggerdb_system_unit=/usr/lib/systemd/system/qloggerdb.service \
		./systemd_unit=/usr/lib/systemd/system/quantumfs.service

# Default to x86_64 location; we'll override when building via mock
RPM_LIBDIR ?= /usr/lib64

RPM_BASENAME_CLIENT := QuantumFS-client
RPM_BASENAME_CLIENT_DEVEL := QuantumFS-client-devel
RPM_FILE_PREFIX_CLIENT := $(RPM_BASENAME_CLIENT)-$(RPM_VERSION)-$(RPM_RELEASE)
RPM_FILE_PREFIX_CLIENT_DEVEL := $(RPM_BASENAME_CLIENT_DEVEL)-$(RPM_VERSION)-$(RPM_RELEASE)

RPM_FILES_TOOLSV2_I686 += $(RPM_FILE_PREFIX_CLIENT).i686.rpm $(RPM_FILE_PREFIX_CLIENT_DEVEL).i686.rpm
RPM_FILES_TOOLSV2_X86_64 += $(RPM_FILE_PREFIX_CLIENT).x86_64.rpm $(RPM_FILE_PREFIX_CLIENT_DEVEL).x86_64.rpm

clientRPM: check-fpm qfsclient
	fpm --force -s dir -t rpm -n $(RPM_BASENAME_CLIENT) \
		--maintainer 'quantumfs-dev@arista.com' \
		--license='Arista Proprietary' \
		--vendor='Arista Networks' \
		--url http://gut/repos/quantumfs \
		--description='QuantumFS client API' \
		--depends jansson \
		--depends openssl \
		--depends libstdc++ \
		--version $(RPM_VERSION) \
		--iteration $(RPM_RELEASE) \
		QFSClient/libqfsclient.so=$(RPM_LIBDIR)/libqfsclient.so
	fpm --force -s dir -t rpm -n $(RPM_BASENAME_CLIENT_DEVEL) \
		--maintainer 'quantumfs-dev@arista.com' \
		--license='Arista Proprietary' \
		--vendor='Arista Networks' \
		--url http://gut/repos/quantumfs \
		--description='Development files for QuantumFS client API' \
		--depends $(RPM_BASENAME_CLIENT) \
		--version $(RPM_VERSION) \
		--iteration $(RPM_RELEASE) \
		QFSClient/qfs_client.h=/usr/include/qfs_client.h

clientRPM32:
	@echo "Building i686 RPMs using mock. This can take several minutes"
	{ \
		set -e ; \
		MOCKLOCK=/tmp/fedora-18-i386.lock ; \
		trap 'rm -f $$MOCKLOCK' EXIT ; \
		(flock 9 || exit 1 ; \
			mock -r fedora-18-i386 --init ; \
			mock -r fedora-18-i386 --install sudo procps-ng git gtest-devel jansson-devel openssl-devel ruby-devel rubygems ; \
			mock -r fedora-18-i386 --shell "sudo gem install --no-ri --no-rdoc fpm" ; \
			mock -r fedora-18-i386 --copyin . /quantumfs ; \
			mock -r fedora-18-i386 --shell "export PATH=$$PATH:/usr/local/bin && cd /quantumfs && make clean clientRPM RPM_LIBDIR=/usr/lib" ; \
			mock -r fedora-18-i386 --copyout /quantumfs/$(RPM_FILE_PREFIX_CLIENT).i686.rpm . ; \
			mock -r fedora-18-i386 --copyout /quantumfs/$(RPM_FILE_PREFIX_CLIENT_DEVEL).i686.rpm . ; \
			mock -r fedora-18-i386 --clean ; \
		) 9>$$MOCKLOCK ; \
	}

rpm: $(COMMANDS) qfsRPM quploadRPM clientRPM clientRPM32

push-rpms: $(RPM_FILES_TOOLSV2_I686) $(RPM_FILES_TOOLSV2_X86_64)
	a4 scp $(RPM_FILES_TOOLSV2_I686) dist:/dist/release/ToolsV2/repo/i386/RPMS
	a4 ssh dist /usr/bin/createrepo --update /dist/release/ToolsV2/repo/i386/RPMS
	a4 scp $(RPM_FILES_TOOLSV2_X86_64) dist:/dist/release/ToolsV2/repo/x86_64/RPMS
	a4 ssh dist /usr/bin/createrepo --update /dist/release/ToolsV2/repo/x86_64/RPMS
	@echo
	@echo "If you're refreshing existing RPMs, then on machines which use this repo you should:"
	@echo "   sudo yum clean all"
	@echo "   sudo yum makecache"

.PHONY: check-fpm rpm-ver qfsRPM quploadRPM clientRPM clientRPM32 rpm push-rpms

include QFSClient/Makefile

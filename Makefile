COMMANDS=quantumfsd qfs
PKGS_TO_TEST=daemon qlog

.PHONY: all $(COMMANDS) $(PKGS_TO_TEST)
.NOTPARALLEL:

all: $(COMMANDS) $(PKGS_TO_TEST)

clean:
	rm -f $(COMMANDS)

fetch:
	for cmd in $(COMMANDS); do \
		echo "Fetching $$cmd"; \
		go get arista.com/quantumfs/cmd/$$cmd; \
	done

$(COMMANDS):
	go build arista.com/quantumfs/cmd/$@

$(PKGS_TO_TEST):
	go test arista.com/quantumfs/$@

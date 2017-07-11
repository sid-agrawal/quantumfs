TARGETMAKEFILE="makefile.mk"

ppid:=$(shell ps -o ppid= $$$$)
ROOTDIRNAME:=$(shell echo -e "$(USER)-RootContainer-$(ppid)" | tr -d '[:space:]')
export ROOTDIRNAME


%:
	make -f $(TARGETMAKEFILE) $@ | tee /dev/tty | ./cleanup.sh $(ppid)

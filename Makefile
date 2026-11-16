BIN := $(HOME)/.local/bin

# `make` builds the binary and installs it to ~/.local/bin.
install: build
	install -d $(BIN)
	install -m 755 moviestreamer $(BIN)/moviestreamer

build:
	go build -o moviestreamer .

.PHONY: install build

# `make tv` signs the Tizen app in tv/ and installs it on the TV in developer mode.
TIZEN := $(HOME)/tizen-studio/tools/ide/bin/tizen
SDB := $(HOME)/tizen-studio/tools/sdb
TV ?= 10.0.0.170

tv:
	mkdir -p build
	$(TIZEN) package -t wgt -s moviestreamer -o $(CURDIR)/build -- $(CURDIR)/tv
	rm -f tv/author-signature.xml tv/signature1.xml tv/.manifest.tmp
	$(SDB) connect $(TV)
	$(TIZEN) install -n MovieStreamer.wgt -s $(TV):26101 -- $(CURDIR)/build

.PHONY: tv

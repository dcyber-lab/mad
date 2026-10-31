BIN ?= $(HOME)/.local/bin/mad
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -s -w -X github.com/dcyber-lab/mad/internal/cli.Version=$(VERSION)

build:
	go build -ldflags '$(LDFLAGS)' -o mad .

install:
	mkdir -p $(dir $(BIN))
	go build -ldflags '$(LDFLAGS)' -o $(BIN) .

test:
	go vet ./...
	go test ./...

clean:
	rm -f mad

# Re-record docs/demo.gif (see docs/demo/README.md).
DEMO_DIR ?= /tmp/madrec
demo:
	python3 docs/demo/record.py $(DEMO_DIR)
	python3 docs/demo/render.py $(DEMO_DIR)

# The iOS app: test the core package, generate the Xcode project, build for
# the simulator, ad hoc signed: an unsigned app has no Keychain, so it forgets
# every pairing (see ios/README.md).
ios:
	cd ios/MadKit && swift test
	cd ios && xcodegen generate
	xcodebuild -project ios/mad.xcodeproj -scheme mad -destination 'generic/platform=iOS Simulator' CODE_SIGN_IDENTITY=- build

.PHONY: build install test clean demo ios

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

# The apps: test the core package, generate the Xcode project, build the iPhone app for the
# simulator (ad hoc signed: an unsigned app has no Keychain, so it forgets every pairing, see
# ios/README.md) and the Mac app (signed to run locally).
ios:
	cd ios/MadKit && swift test
	cd ios/MadUI && swift test
	cd ios/MadShell && swift test
	cd ios && xcodegen generate
	xcodebuild -project ios/mad.xcodeproj -scheme mad -destination 'generic/platform=iOS Simulator' CODE_SIGN_IDENTITY=- build
	xcodebuild -project ios/mad.xcodeproj -scheme madmac -destination 'platform=macOS' -derivedDataPath ios/build/mac build

# Build the Mac app and open it. It finds mad's server by itself (see ios/README.md).
mac:
	cd ios && xcodegen generate
	xcodebuild -project ios/mad.xcodeproj -scheme madmac -destination 'platform=macOS' -derivedDataPath ios/build/mac build
	open ios/build/mac/Build/Products/Debug/madmac.app

# A Release build of the Mac app, put in /Applications (replacing the one there).
mac-install:
	cd ios && xcodegen generate
	xcodebuild -project ios/mad.xcodeproj -scheme madmac -configuration Release -destination 'platform=macOS' -derivedDataPath ios/build/mac-release build
	rm -rf /Applications/madmac.app
	cp -R ios/build/mac-release/Build/Products/Release/madmac.app /Applications/madmac.app
	@echo "madmac is in /Applications: open it from Spotlight or the Launchpad."

# Put the app on your iPhone with a free Apple ID: builds Release for the phone,
# installs and launches it. DEVICE=<id|name> and TEAM=<id> pick a phone or a
# team when there are several (see ios/README.md).
ios-device:
	ios/scripts/device.sh

# UI tests of the app and its page, in a simulator of their own ("mad UITest"), against
# the fixture server, with a screenshot report in ios/build/uitest-report/index.html
# (see ios/README.md). ONLY="UsageTests AppTests" runs some of the classes.
ios-uitest:
	ios/scripts/uitest.sh

.PHONY: build install test clean demo ios mac mac-install ios-device ios-uitest

BIN := bin/mobium
VERSION := $(shell cat VERSION 2>/dev/null || echo dev)

.PHONY: all build test fmt fmt-check vet lint clients java crosscompile api api-check flags flags-check quickstart docs-check ci clean

all: build test

build:
	@mkdir -p bin
	go build -ldflags "-X main.version=$(VERSION)" -o $(BIN) ./cmd/mobium
	@echo "built $(BIN)"

# The Go client is a separate module, so ./... does not reach it and it has to
# be named. Forgetting that would mean it never runs in CI.
MODULES := . clients/go

test:
	@for m in $(MODULES); do echo "test $$m"; (cd $$m && go test ./...) || exit 1; done

fmt:
	gofmt -l -w cmd internal clients/go

# fmt-check reports rather than rewrites, because CI must fail on unformatted
# code instead of quietly fixing it and passing.
fmt-check:
	@out=$$(gofmt -l cmd internal clients/go); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "gofmt clean"

vet:
	@for m in $(MODULES); do (cd $$m && go vet ./...) || exit 1; done

# The version is pinned so a lint failure is a change in this repo and never
# an upgrade landing underneath it. Bump deliberately, and expect new findings
# when you do.
GOLANGCI_VERSION := 2.13.2
GOLANGCI := $(shell go env GOPATH)/bin/golangci-lint

lint:
	@if ! [ -x "$(GOLANGCI)" ]; then 		echo "golangci-lint is not installed. Run: make lint-install"; exit 1; 	fi
	@have=$$("$(GOLANGCI)" version 2>/dev/null | sed -n 's/.*has version \([0-9.]*\).*/\1/p'); 	if [ "$$have" != "$(GOLANGCI_VERSION)" ]; then 		echo "golangci-lint $$have installed, $(GOLANGCI_VERSION) pinned. Run: make lint-install"; exit 1; 	fi
	@for m in $(MODULES); do (cd $$m && "$(GOLANGCI)" run ./...) || exit 1; done

# Downloads the pinned release and verifies it against the checksum the
# release itself publishes.
#
# Not upstream's install.sh: its checksum step greps the checksums file for
# the archive name, which also matches the `.sbom.json` line, so it compares
# the SBOM's hash against the tarball's and reports a mismatch on a download
# that is in fact genuine. An integrity check that fails for the wrong reason
# is no better than one that passes for the wrong reason. The `\$$` below
# anchors the match to the end of the line, which is the whole fix.
lint-install:
	@os=$$(go env GOOS); arch=$$(go env GOARCH); 	v=$(GOLANGCI_VERSION); base=golangci-lint-$$v-$$os-$$arch; 	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; 	url=https://github.com/golangci/golangci-lint/releases/download/v$$v; 	echo "downloading $$base"; 	curl -sSfL "$$url/$$base.tar.gz" -o "$$tmp/$$base.tar.gz"; 	curl -sSfL "$$url/golangci-lint-$$v-checksums.txt" -o "$$tmp/sums.txt"; 	want=$$(grep " $$base\.tar\.gz$$" "$$tmp/sums.txt" | awk '{print $$1}'); 	got=$$(shasum -a 256 "$$tmp/$$base.tar.gz" | awk '{print $$1}'); 	if [ -z "$$want" ]; then echo "no checksum published for $$base"; exit 1; fi; 	if [ "$$want" != "$$got" ]; then 		echo "checksum mismatch for $$base"; echo "  got  $$got"; echo "  want $$want"; exit 1; 	fi; 	echo "checksum verified"; 	tar xzf "$$tmp/$$base.tar.gz" -C "$$tmp"; 	install -m 0755 "$$tmp/$$base/golangci-lint" "$(GOLANGCI)"; 	"$(GOLANGCI)" version

# clients checks the two non-Go clients parse, and runs the one thing each can
# test without a device: how a failed tool call becomes an exception. They
# have no suite beyond that — what they do is verified against a device by
# hand. The error tests need no framework, only node and python3.
clients:
	node --check clients/javascript/index.js
	@python3 -c "import ast, pathlib, sys; \
	    [ast.parse(f.read_text(), str(f)) for f in pathlib.Path('clients/python').rglob('*.py')]" \
	  || exit 1
	@echo "clients parse"
	@node clients/javascript/test/errors.test.mjs
	@node clients/javascript/test/connection.test.mjs
	@python3 clients/python/tests/test_errors.py
	@python3 clients/python/tests/test_connection.py

# crosscompile is the check that Windows and Linux still build. Windows has no
# daemon transport (see docs/WINDOWS.md) but everything must still compile for
# it, and that has broken before without anyone on a Mac noticing.
CROSS := windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

crosscompile:
	@for t in $(CROSS); do \
		os=$${t%/*}; arch=$${t#*/}; \
		GOOS=$$os GOARCH=$$arch go build -o /dev/null ./cmd/mobium || exit 1; \
		(cd clients/go && GOOS=$$os GOARCH=$$arch go build ./...) || exit 1; \
		echo "  $$t"; \
	done
	@echo "cross-compiles"

# java builds, tests and packages the Java client the way it is published:
# through the Maven wrapper, which fetches a pinned, checksum-verified Maven
# on first use. `verify` compiles with -Xlint:all -Werror, runs the tests,
# builds the jar, sources and javadoc jars with javadoc's doclint on and
# warnings fatal, and fails if the client has acquired a dependency. Skipped
# with a note when there is no JDK, because most people working on the Go
# here will not have one and should not be blocked by it.
JAVA_HOME_GUESS := $(shell /usr/libexec/java_home 2>/dev/null || echo /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home)
JAVA  := $(shell command -v java  2>/dev/null || echo $(JAVA_HOME_GUESS)/bin/java)

java:
	@if [ ! -x "$(JAVA)" ]; then \
		echo "no JDK found, skipping the Java client (set JAVA_HOME or install one)"; \
		exit 0; \
	fi; \
	cd clients/java && ./mvnw -B --no-transfer-progress -q verify

# ci is everything that runs without a device. Keep this the single definition
# of that, so the workflow and a person checking before a commit cannot drift.
# api regenerates docs/API.md and docs/api/surface.json from the source. The
# table is 35 tools across five surfaces; maintained by hand it would be wrong
# within a week.
api:
	@go run ./internal/apisurface/generate .
	@echo "api surface written"

# api-check fails if the generated files are out of date, so a tool added
# without regenerating them is caught in CI rather than read as truth later.
api-check:
	@go run ./internal/apisurface/generate .
	@git diff --quiet -- docs/API.md docs/api/surface.json || { \
		echo "docs/API.md or docs/api/surface.json is stale — run 'make api' and commit the result"; \
		git --no-pager diff --stat -- docs/API.md docs/api/surface.json; \
		exit 1; }
	@echo "api surface current"

# The .NET client. Skips rather than fails when there is no SDK, matching the
# Java target: a machine doing Android work has no reason to have one.
#
# dotnet build, then the test binary. No test framework and no NuGet restore
# beyond the SDK's own packs, so this needs no network after the SDK is there.
DOTNET := $(shell command -v dotnet 2>/dev/null || echo $(HOME)/.dotnet/dotnet)

dotnet:
	@if [ ! -x "$(DOTNET)" ]; then \
		echo "no .NET SDK found, skipping the .NET client (install one, or see clients/dotnet/README.md)"; \
		exit 0; \
	fi; \
	"$(DOTNET)" build clients/dotnet/Mobium/Mobium.csproj -v q --nologo && \
	"$(DOTNET)" run --project clients/dotnet/Tests -v q --nologo

# The flag surface: what each tool accepts, and which command sets it. Written
# by the same generator as the API surface, because it is the same procedure
# one level down and a second generator would be a second thing to keep in step.
flags: api

# flags-check fails if the generated flag documents are out of date. Separate
# from api-check because they answer different questions and a failure should
# name which one drifted -- and because api-check diffs only its own two files,
# so a stale FLAGS.md would otherwise sail through.
flags-check:
	@go run ./internal/apisurface/generate .
	@git diff --quiet -- docs/FLAGS.md docs/api/flags.json || { \
		echo "docs/FLAGS.md or docs/api/flags.json is stale — run 'make flags' and commit the result"; \
		git --no-pager diff --stat -- docs/FLAGS.md docs/api/flags.json; \
		exit 1; }
	@echo "flag surface current"

# The documentation checks. Both existed for months and neither was wired to
# anything: the spelling sweep was never invoked by the Makefile or by any
# workflow, so `make ci` had never once checked spelling, and it silently
# scanned nothing when run with no arguments. Both tools now refuse an empty
# file list, and both are passed every tracked file rather than a hand-written
# list of extensions -- the list was the part that was wrong, having omitted
# '*.sh' while a British spelling sat in one of the check scripts. The word
# itself is deliberately not written here: this target scans the Makefile too,
# and a tool whose input vocabulary is the thing it removes will consume its
# own documentation. See CHALLENGES, "a spelling sweep that ate its own
# documentation".
# quickstart builds the per-client quick-start pages from their examples and
# the output those examples printed on a real emulator and simulator, so a page
# cannot show code that did not run. docs-check fails if one is out of date.
quickstart:
	@python3 docs/quickstart/build.py

docs-check:
	@python3 docs/checks/american-spelling.py scan $$(git ls-files) >/dev/null \
		|| { python3 docs/checks/american-spelling.py scan $$(git ls-files); exit 1; }
	@python3 docs/checks/doc-links.py $$(git ls-files '*.md') >/dev/null \
		|| { python3 docs/checks/doc-links.py $$(git ls-files '*.md'); exit 1; }
	@python3 docs/quickstart/build.py --check >/dev/null \
		|| { python3 docs/quickstart/build.py --check; exit 1; }
	@echo "docs: spelling, anchors and quick-start pages clean"

ci: fmt-check vet lint test clients crosscompile java api-check flags-check docs-check dotnet

clean:
	rm -rf bin clients/java/target

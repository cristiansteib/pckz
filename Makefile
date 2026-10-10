# Version is injected by the release workflow (semantic-release).
VERSION ?= 0.0.0-dev
# owner/name of the GitHub repository releases are published to; enables the
# in-app update check. GitHub Actions provides it; source builds leave it empty.
REPO    ?= $(GITHUB_REPOSITORY)
ARCHES  ?= amd64 arm64
BIN      = bin/audio-switcher
LDFLAGS  = -s -w -X main.version=$(VERSION) -X main.repo=$(REPO)
GOBUILD  = CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)'

.PHONY: build run demo test lint icons deb dist install clean

# Pure Go, no cgo: GTK 4 and libadwaita are loaded at runtime (puregotk).
build:
	$(GOBUILD) -o $(BIN) ./cmd/audio-switcher

run: build
	./$(BIN)

# Fake devices, no real audio touched — for screenshots and UI work.
demo: build
	AUDIO_SWITCHER_DEMO=1 ./$(BIN)

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run gofmt -w ."; exit 1; }
	go vet ./...

icons:
	go run ./tools/genicons

# .deb for this machine's architecture.
deb: build
	packaging/build-deb.sh $(VERSION) $$(dpkg --print-architecture) $(BIN) dist

# Release artifacts for every architecture: tarballs (mise / manual) and .debs.
dist:
	rm -rf dist build
	set -e; for arch in $(ARCHES); do \
		name=audio-switcher_$(VERSION)_linux_$$arch; \
		GOOS=linux GOARCH=$$arch $(GOBUILD) -o build/$$name/audio-switcher ./cmd/audio-switcher; \
		cp README.md LICENSE build/$$name/; \
		mkdir -p dist; tar -C build -czf dist/$$name.tar.gz $$name; \
		packaging/build-deb.sh $(VERSION) $$arch build/$$name/audio-switcher dist; \
	done
	cd dist && sha256sum * > checksums.txt

install: deb
	sudo apt install ./dist/pckz-audio-switcher_$(VERSION)_$$(dpkg --print-architecture).deb

clean:
	rm -rf bin build dist

ifeq ($(OS),Windows_NT)
    SOURCES := $(shell dir /S /B *.go)
else
    SOURCES := $(shell find . -name '*.go')
endif

ifeq ($(shell uname),Darwin)
    GOOS = darwin
    GOARCH = amd64
    EXEEXT =
else ifeq ($(shell uname),Linux)
    GOOS = linux
    GOARCH = $(shell arch)
    EXEEXT =
else ifeq ($(OS),Windows_NT)
    GOOS = windows
    GOARCH = amd64
    EXEEXT = .exe
endif

APP := bluetrack$(EXEEXT)
TARGET := ./dist/bluetrack_$(GOOS)_$(GOARCH)_v1/$(APP)

$(APP): $(TARGET)
	cp $< $@

run: $(APP)
	./$(APP) --config network.yaml --container csls --script firewall.sh --security-group-name northflier --terraform sg_rules.tf 2>&1 | tee log.txt

$(TARGET): $(SOURCES)
	gofumpt -w $(SOURCES)
	goreleaser build --single-target --snapshot --clean
	go vet ./...

all:
	goreleaser build --snapshot --clean

.PHONY: clean
clean:
	rm -f sg_rules.tf
	rm -f firewall.sh
	rm -f lxd_config.yaml
	rm -f log.txt
	rm -f bluetrack
	rm -f $(TARGET)
	rm -rf dist

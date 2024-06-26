# SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
#
# SPDX-License-Identifier: Apache-2.0


export CGO_ENABLED=0
export GO111MODULE=on

.PHONY: build

HELMIT_VERSION := latest

build-tools:=$(shell if [ ! -d "./build/build-tools" ]; then cd build && git clone https://github.com/onosproject/build-tools.git; fi)
include ./build/build-tools/make/onf-common.mk

build: # @HELP build the Go binaries and run all validations (default)
build: build-helmit

build-helmit:
	go build -o build/_output/helmit .

test: # @HELP run the unit tests and source code validation
test: linters license build deps
	go test github.com/onosproject/helmit/...

jenkins-test:  # @HELP run the unit tests and source code validation producing a junit style report for Jenkins
jenkins-test: deps license linters
	TEST_PACKAGES=NONE ./build/build-tools/build/jenkins/make-unit

images: # @HELP build all Docker images
images:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o build/helmit-runner/_output/bin/helmit-runner ./cmd/helmit-runner
	docker build build/helmit-runner -f build/helmit-runner/Dockerfile \
		--build-arg ONOS_BUILD_VERSION=${ONOS_BUILD_VERSION} \
		-t onosproject/helmit-runner:${HELMIT_VERSION}

kind: # @HELP build Docker images and add them to the currently configured kind cluster
kind: images
	@if [ "`kind get clusters`" = '' ]; then echo "no kind cluster found" && exit 1; fi
	kind load docker-image onosproject/helmit-runner:${HELMIT_VERSION}

all: build images tests

publish: # @HELP publish version on github and dockerhub
	./build/build-tools/publish-version ${VERSION} onosproject/helmit-runner

jenkins-publish: # @HELP Jenkins calls this to publish artifacts
	./build/bin/push-images
	./build/build-tools/release-merge-commit

all: t
clean:: # @HELP remove all the build artifacts
	rm -rf ./build/_output ./vendor

# export GO111MODULE=on
current_dir = $(shell pwd)

SHELL = /bin/sh

ifneq ($(OS), Windows_NT)
	CURRENT_PLATFORM = linux
	ifeq ($(shell uname), Darwin)
		SHELL := /bin/sh
		CURRENT_PLATFORM = darwin
	endif
else
	CURRENT_PLATFORM = windows
endif

all: xp-build

.PHONY: postgres

.pre-build:
	mkdir -p build/darwin
	mkdir -p build/linux

lint:
	golangci-lint run

fix:
	golangci-lint run --fix

gomodcheck:
	@go help mod > /dev/null || (@echo mdmdirector requires Go version 1.11 or  higher for module support && exit 1)

deps: gomodcheck
	@go mod download

clean:
	rm -rf build

build: clean .pre-build
	echo "Building..."
	go build -o build/$(CURRENT_PLATFORM)/mdmdirector

xp-build:  clean .pre-build
	GOOS=darwin go build -o build/darwin/mdmdirector
	GOOS=linux CGO_ENABLED=0 go build -o build/linux/mdmdirector

postgres-clean:
	rm -rf postgres

postgres:
	docker rm -f mdmdirector-postgres || true
	docker run --name mdmdirector-postgres -p 5432:5432 -e POSTGRES_PASSWORD=password -v ${current_dir}/postgres:/var/lib/postgresql/data -d postgres:11
	docker rm -f mdmdirector-redis || true
	docker run --name mdmdirector-redis -d -p 6379:6379 redis
	sleep 5

mdmdirector_nosign: build
	build/$(CURRENT_PLATFORM)/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug

curlprofile:
	rm -f EnrollmentProfile.mobileconfig
	curl -o EnrollmentProfile.mobileconfig ${SERVER_URL}/mdm/enroll

mdmdirector: build curlprofile
	build/$(CURRENT_PLATFORM)/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug -sign -cert=SigningCert.p12 -key-password=password -password=secret  -db-username=postgres -db-host=127.0.0.1 -db-port=5432 -db-name=postgres -db-password=password -db-sslmode=disable -loglevel=debug -escrowurl="${ESCROW_URL}" -clear-device-on-enroll -enrollment-profile=EnrollmentProfile.mobileconfig -enrollment-profile-signed -prometheus #-log-format=json

test:
	go test -cover -v ./...
export GO111MODULE=on
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

clean:
	rm -rf build

build: clean .pre-build
	echo "Building..."
	go build -o build/$(CURRENT_PLATFORM)/mdmdirector

xp-build:  clean .pre-build
	GOOS=darwin go build -o build/darwin/mdmdirector
	GOOS=linux CGO_ENABLED=0 go build -o build/linux/mdmdirector-v1

postgres-clean:
	rm -rf postgres

postgres:
	docker rm -f mdmdirector-postgres || true
	docker run --name mdmdirector-postgres -p 5432:5432 -e POSTGRES_PASSWORD=password -v ${current_dir}/postgres:/var/lib/postgresql/data -d postgres:11
	sleep 5

mdmdirector_nosign: build
	build/$(CURRENT_PLATFORM)/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug

mdmdirector: build
	build/$(CURRENT_PLATFORM)/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug -sign -cert=SigningCert.p12 -key-password=password -password=secret -dbconnection="host=127.0.0.1 port=5432 user=postgres dbname=postgres password=password sslmode=disable" -loglevel=debug
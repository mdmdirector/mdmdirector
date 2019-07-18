export GO111MODULE=on
current_dir = $(shell pwd)

all: mdmdirector

.PHONY: postgres

clean:
	rm -rf build

build: clean
	echo "Building..."
	go build -o build/mdmdirector

postgres-clean:
	rm -rf postgres

postgres:
	docker rm -f mdmdirector-postgres || true
	docker run --name mdmdirector-postgres -p 5432:5432 -e POSTGRES_PASSWORD=password -v ${current_dir}/postgres:/var/lib/postgresql/data -d postgres:11
	sleep 5

mdmdirector_nosign: build
	build/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug

mdmdirector: build
	build/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug -sign -cert=SigningCert.p12 -password=password
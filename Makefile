 export GO111MODULE=on

all: mdmdirector

clean:
	rm -rf build

build: clean
	echo "Building..."
	go build -o build/mdmdirector

mdmdirector: build
	build/mdmdirector -micromdmurl="${SERVER_URL}" -micromdmapikey="supersecret" -debug
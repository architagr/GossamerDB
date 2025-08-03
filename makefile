.PHONY: build build-coordinator build-datanode run run-coordinator run-datanode clean

build:
	@echo "Building the project..."
	make clean
	make build-coordinator
	make build-datanode

build-coordinator:
	@echo "Building the coordinator..."
	go build -o ./build/coordinator ./cmd/coordinator 

build-datanode:
	@echo "Building the datanode..."
	go build -o ./build/datanode ./cmd/datanode 

run:
	@echo "Running the project..."
	make run-coordinator
	nake run-datanode

run-coordinator:
	@echo "Running the coordinator..."
	go run ./cmd/coordinator/main.go --config-file-path=/Users/architagarwal/code/GossamerDB/config.yaml --node-id="coodinator-node-1"

run-datanode:
	@echo "Running the datanode..."
	go run ./cmd/datanode/main.go --config-file-path=/Users/architagarwal/code/GossamerDB/config.yaml --node-id="data-node-1"

clean:
	rm -rf ./build
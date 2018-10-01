# GOPATH=$(shell pwd)
# SEED_ADDRS="dc1/127.0.0.1:7001"


build:
	go build -v -o bin/qtun main.go

run1:
	go run -v src/meshbird/cmd/main.go \
		--seed_addrs "${SEED_ADDRS}" \
		--local_addr "127.0.0.1:7001" \
		--ip 10.237.0.1/16
run2:
	go run -v src/meshbird/cmd/main.go \
		--seed_addrs "${SEED_ADDRS}" \
		--local_addr "127.0.0.1:7002" \
		--ip 10.237.0.2/16
run3:
	go run -v src/meshbird/cmd/main.go \
		--seed_addrs "${SEED_ADDRS}" \
		--local_addr "127.0.0.1:7003" \
		--ip 10.237.0.3/16
	
deps:
	go get -v meshbird

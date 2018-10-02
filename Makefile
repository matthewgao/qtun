# GOPATH=$(shell pwd)
# SEED_ADDRS="dc1/127.0.0.1:7001"


build:
	go build -v -o bin/qtun main.go

deps:
	go get -v qtun

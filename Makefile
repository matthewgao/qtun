# GOPATH=$(shell pwd)

build:
	go build -v -o bin/qtun main.go

windows:
	env GOOS=windows GOARCH=amd64 go build -v -o bin/qtun-win main.go

linux:
	env GOOS=linux GOARCH=amd64 go build -v -o bin/qtun-linux main.go

deps:
	go get -v qtun

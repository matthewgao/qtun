# GOPATH=$(shell pwd)

build:
	go build -v -o bin/qtun main.go

windows:
	env GOOS=windows GOARCH=amd64 go build -v -o bin/qtun-win main.go

linux:
	env GOOS=linux GOARCH=amd64 go build -v -o bin/qtun-linux main.go

linux-i686:
	env GOOS=linux GOARCH=386 go build -v -o bin/qtun-linux main.go

arm:
	env GOOS=linux GOARM=7 GOARCH=arm go build -v -o bin/qtun-arm main.go

deps:
	go get -v qtun

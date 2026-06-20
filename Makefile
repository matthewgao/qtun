# GOPATH=$(shell pwd)

build:
	go build -v -o bin/qtun main.go

# Windows 运行需要 wintun.dll 与 exe 同目录（从 https://www.wintun.net 下载对应架构）。
windows:
	env GOOS=windows GOARCH=amd64 go build -v -o bin/qtun-win.exe main.go

windows-arm64:
	env GOOS=windows GOARCH=arm64 go build -v -o bin/qtun-win-arm64.exe main.go

linux:
	env GOOS=linux GOARCH=amd64 go build -v -o bin/qtun-linux main.go

linux-i686:
	env GOOS=linux GOARCH=386 go build -v -o bin/qtun-linux main.go

arm:
	env GOOS=linux GOARM=7 GOARCH=arm go build -v -o bin/qtun-arm main.go

m4:
	env GOOS=darwin GOARCH=arm64 go build -v -o bin/qtun-m4 main.go

deps:
	go get -v qtun

.PHONY: build test app install run clean

build:
	swift build

test:
	swift test

app:
	bash scripts/bundle.sh

install: app
	rm -rf /Applications/ProxyBar.app
	cp -R build/ProxyBar.app /Applications/

run: app
	open build/ProxyBar.app

clean:
	rm -rf .build build
file:=exporter
linux:
	GOOS=linux GOARCH=amd64 go build -o dist/$(file)-linux .

osx:
	GOOS=darwin GOARCH=amd64 go build -o dist/$(file)-ox .

release: linux osx

.PHONY: release linux osx

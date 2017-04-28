distname=nginx-plus-exporter
package=github.com/chototoss/nginx-plus-exporter
goversion=1.8.0

build:
	docker run -v `pwd`:/go/src/$(package) golang:$(goversion)-alpine go build -o /go/src/$(package)/dist/$(distname) -v $(package)

.PHONY: build

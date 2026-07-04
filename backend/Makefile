LAMBDAS := api pager canary
GOFLAGS := CGO_ENABLED=0 GOOS=linux GOARCH=arm64

.PHONY: build test vet clean $(LAMBDAS)

build: $(LAMBDAS)

$(LAMBDAS):
	mkdir -p dist/$@
	$(GOFLAGS) go build -tags lambda.norpc -ldflags="-s -w" -o dist/$@/bootstrap ./cmd/$@

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -rf dist

lint: fmt
	golangci-lint run ./... --timeout 10m -v -c .golangci.yml

fmt:
	gofumpt -l -w .
	gci -w -local github.com/xenking/exchange-emulator .

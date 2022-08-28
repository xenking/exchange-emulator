lint: fmt ## Run formatter and linter
	golangci-lint run ./... --timeout 10m -v -c .golangci.yml

fmt: ## Format the code
	gofumpt -l -w .
	gci write -s standard -s default -s "prefix(github.com/xenking/exchange-emulator)" .

pt_gen_path = ./gen/proto/api
pt_spec_path = ./api/proto/*.proto

generate: ## Generate models
	rm -rf ./gen/proto/api/*
	protoc  -I ./api/proto/include \
			-I ./api/proto \
			--proto_path=api/proto \
			--go_out=$(pt_gen_path) \
			--go_opt paths=source_relative \
	   	  	--go-grpc_out=$(pt_gen_path) \
	   	  	--go-grpc_opt paths=source_relative \
			$(pt_spec_path)

# Absolutely awesome: http://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
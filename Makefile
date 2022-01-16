lint: fmt
	golangci-lint run ./... --timeout 10m -v -c .golangci.yml

fmt:
	gofumpt -l -w .
	gci -w -local github.com/xenking/exchange-emulator .


pt_gen_path = ./gen/proto/api
pt_spec_path = ./api/proto/*.proto

generate:
	rm -rf ./gen/proto/api/*
	protoc  -I ./api/proto/include \
			--proto_path=api/proto \
			--go_out=$(pt_gen_path) \
			--go_opt paths=source_relative \
	   	  	--go-grpc_out=$(pt_gen_path) \
	   	  	--go-grpc_opt paths=source_relative \
			$(pt_spec_path)
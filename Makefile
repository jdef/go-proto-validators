# Copyright 2016 Michal Witkowski. All Rights Reserved.
# See LICENSE for licensing terms.

export PATH := ${GOPATH}/bin:${PATH}

.PHONY: install
install:
	@echo "--- Installing govalidators to GOPATH"
	go install github.com/mwitkow/go-proto-validators/protoc-gen-govalidators

.PHONY: regenerate_test_gogo
regenerate_test_gogo:
	@echo "Regenerating test .proto files with gogo imports"
	(protoc  \
	--proto_path=${GOPATH}/src \
 	--proto_path=test \
	--gogo_out=test/gogo \
	--govalidators_out=gogoimport=true:test/gogo test/*.proto)

.PHONY: regenerate_test_golang
regenerate_test_golang:
	@echo "--- Regenerating test .proto files with golang imports"
	(protoc  \
	--proto_path=${GOPATH}/src \
 	--proto_path=test \
	--go_out=test/golang \
	--govalidators_out=test/golang test/*.proto)

.PHONY: regenerate_example
regenerate_example: install
	@echo "--- Regenerating example directory"
	(protoc  \
	--proto_path=${GOPATH}/src \
	--proto_path=. \
	--go_out=. \
	--govalidators_out=. examples/*.proto)

.PHONY: test
test: install regenerate_test_gogo regenerate_test_golang
	@echo "Running tests"
	go test -count=1 -v ./...

.PHONY: regenerate
regenerate:
	@echo "--- Regenerating validator.proto"
	(cd ${GOPATH}/src && protoc \
	--proto_path=. \
	--proto_path=${GOPATH}/src/github.com/gogo/protobuf/protobuf \
	--go_out=paths=source_relative,Mgoogle/protobuf/descriptor.proto=github.com/golang/protobuf/protoc-gen-go/descriptor:. \
	github.com/mwitkow/go-proto-validators/validator.proto \
	)

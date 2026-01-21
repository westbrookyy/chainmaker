VERSION=v2.3.6

build:
	go mod tidy && go build ./...

gomod:
	go get chainmaker.org/chainmaker/chainconf/v2@v2.3.5_qc
	go get chainmaker.org/chainmaker/common/v2@v2.3.8_qc
	go get chainmaker.org/chainmaker/consensus-utils/v2@v2.3.4_qc
	go get chainmaker.org/chainmaker/localconf/v2@v2.3.7_qc
	go get chainmaker.org/chainmaker/logger/v2@v2.3.4_qc
	go get chainmaker.org/chainmaker/lws@v1.2.1
	go get chainmaker.org/chainmaker/pb-go/v2@v2.3.6_qc
	go get chainmaker.org/chainmaker/protocol/v2@v2.3.9_qc
	go get chainmaker.org/chainmaker/utils/v2@v2.3.5_qc
	go mod tidy

lint:
	golangci-lint run ./...
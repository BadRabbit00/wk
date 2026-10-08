lint:
    test -z "$(gofmt -l cmd internal)"
    go vet ./...

test:
    go test -coverpkg=./... -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out > coverage-summary.txt
    cat coverage-summary.txt
    awk '$NF != "100.0%" { failed = 1 } END { exit failed }' coverage-summary.txt

build:
    nix build .#wk

{ pkgs }:
let
  binaryName = "wk";
  source = pkgs.lib.cleanSource ./.;
in pkgs.buildGoModule {
  pname = binaryName;
  version = "0.1.0";
  src = source;
  ldflags = [ "-X wk-cli/internal/wk.defaultFlake=path:${source}" ];
  vendorHash = "sha256-g+yaVIx4jxpAQ/+WrGKxhVeliYx7nLQe/zsGpxV4Fn4=";
  env.CGO_ENABLED = "0";
  subPackages = [ "cmd/cli" ];
  nativeCheckInputs = [ pkgs.git ];
  checkPhase = ''
    runHook preCheck
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    go test -coverprofile=coverage.out -coverpkg=./... ./...
    go tool cover -func=coverage.out > coverage-summary.txt
    cat coverage-summary.txt
    awk '$NF != "100.0%" { failed = 1 } END { exit failed }' coverage-summary.txt
    runHook postCheck
  '';
  postInstall = ''
    mv $out/bin/cli $out/bin/${binaryName}
  '';
  meta.mainProgram = binaryName;
}

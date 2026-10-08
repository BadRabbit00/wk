{
  description = "wk: project CLI for wikisvc";
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs = { self, nixpkgs }:
    let
      eachSystem = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
    in {
      packages = eachSystem (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in rec {
          wk = import ./package.nix { inherit pkgs; };
          default = wk;
        });
      apps = eachSystem (system: rec {
        wk = {
          type = "app";
          program = nixpkgs.lib.getExe self.packages.${system}.wk;
          meta.description = "Project setup and diagnostics for wikisvc";
        };
        default = wk;
      });
      checks = eachSystem (system: { wk = self.packages.${system}.wk; });
      devShells = eachSystem (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in {
          default = pkgs.mkShell {
            packages = [ pkgs.go pkgs.git pkgs.just ];
            env.CGO_ENABLED = "0";
          };
        });
    };
}

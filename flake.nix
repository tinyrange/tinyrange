{
  description = "A basic gomod2nix flake";

  inputs = {
    nixpkgs.url = "nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    gomod2nix = {
      url = "github:tweag/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
    flake-utils.lib.eachSystem [
      "x86_64-linux"
    ] (system: 
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            gomod2nix.overlays.default
          ];
        };

        everything = pkgs.buildGoApplication {
          pname = "pkg2";
          version = "0.0.1";
          src = ./.;
          modules = ./gomod2nix.toml;
        };
      in {
        packages = rec {
          default = everything;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gomod2nix.packages.${system}.default
          ];
        };
      }
    );
}

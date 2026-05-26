{
  description = "Umbra VPN controller — local overlay and NixOS module";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
  let
    system = "x86_64-linux";
    pkgs = import nixpkgs { inherit system; overlays = [ self.overlays.umbra ]; };
  in {
    packages.${system} = {
      umbra = pkgs.callPackage ./pkgs/umbra/default.nix {};
      umbra-headless = pkgs.callPackage ./pkgs/umbra/default.nix { buildHeadless = true; };
    };

    # Usage — add to your flake inputs:
    #   overlays = [ (import ./flake.nix).outputs.overlays.umbra ];
    overlays.umbra = final: prev: {
      umbra = final.callPackage ./pkgs/umbra/default.nix {};
      umbra-headless = final.callPackage ./pkgs/umbra/default.nix { buildHeadless = true; };
    };

    # Usage — add to your module imports (overlay auto-imported):
    #   imports = [ (import ./flake.nix).outputs.nixosModules.umbra ];
    # Then enable with:
    #   services.umbra.enable = true;
    nixosModules.umbra = import ./modules/umbra.nix;
  };
}

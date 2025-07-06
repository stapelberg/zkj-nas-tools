{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
  };

  outputs = { self, nixpkgs, ... }: {
    nixosModules.zkjbackup = import ./backupnixos/zkjbackup.nix;
    nixosModules.dramaqueen = import ./dramaqueen/dramaqueen.nix;
  };
}

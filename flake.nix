{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs";
  };

  outputs = { self, nixpkgs, ... }: {
    nixosModules.zkjbackup = import ./backupnixos/zkjbackup.nix;
  };
}

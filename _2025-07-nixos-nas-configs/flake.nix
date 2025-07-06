{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-25.05";

    stapelbergnix.url = "github:stapelberg/nix";
  };

  outputs =
    {
      self,
      nixpkgs,
      stapelbergnix,
    }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfree = false;
      };
    in
    {

      nixosConfigurations.storage2 = nixpkgs.lib.nixosSystem {
        inherit system;
        inherit pkgs;
        modules = [
          ./configuration.nix
          stapelbergnix.lib.userSettings
          # We have our own networking config
          # stapelbergnix.lib.systemdNetwork
          # Use systemd-boot as bootloader
          stapelbergnix.lib.systemdBoot
        ];
      };
      formatter.${system} = pkgs.nixfmt-tree;
    };
}

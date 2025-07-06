.PHONY: deploy build update

all:
	echo "make update or make deploy"

update:
	nix flake update

build:
	nix run nixpkgs#nixos-rebuild -- build --flake .#storage2

deploy:
	nix run nixpkgs#nixos-rebuild -- --target-host michael@storage2 --use-remote-sudo switch --flake .#storage2

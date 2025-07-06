{
  config,
  lib,
  pkgs,
  ...
}:

let
  syncpl = import ./syncpl.nix { pkgs = pkgs; };
in
{
  imports = [
    ./hardware-configuration.nix
    ./jellyfin.nix
    ./samba.nix
    ./immich.nix
    ./crypto-unlock.nix
  ];

  networking.hostName = "storage2";

  boot.kernelModules = [
    "nct6775" # to make hardware sensors available
  ];

  security.sudo.wheelNeedsPassword = false;
  users.mutableUsers = false;
  users.groups.michael = {
    gid = 1000; # for consistency with storage3
  };
  users.users.michael = {
    extraGroups = [
      "wheel" # Enable ‘sudo’ for the user.
      "docker"
      # By default, NixOS does not add users to their own group:
      # https://github.com/NixOS/nixpkgs/issues/198296
      "michael"
    ];
    initialPassword = "secret"; # random; never used
  };

  users.users.root.openssh.authorizedKeys.keys = [
    ''command="${pkgs.rrsync}/bin/rrsync /srv/backup/midna"            ssh-ed25519 AAAAC3Npublickey root@midna''

    ''command="${syncpl}/bin/syncpl",no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAAC3Npublickey sync@dr''

    ''command="${pkgs.systemd}/sbin/poweroff",no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAAC3Npublickey suspend@dr''
  ];

  environment.systemPackages = with pkgs; [
    cryptsetup
    tailscale
    prometheus-node-exporter
    ethtool # for verifying wake-on-LAN status
    ncdu
    screen
    lshw
    usbutils
    smartmontools
    htop
    jellyfin
    sqlite # for debugging jellyfin databases
    rrsync # for backups
    syncpl
  ];

  programs.ssh.knownHosts = {
    "storage3-ed25519" = {
      hostNames = [ "10.0.0.253" ];
      publicKey = "ssh-ed25519 AAAAC3Npublickey";
    };
  };

  # Use systemd for networking
  services.resolved.enable = true;
  networking.useDHCP = false;
  systemd.network.enable = true;

  # The Mellanox network card does not support WOL :(
  # networking.interfaces.enp9s0.wakeOnLan.enable = true;

  # systemd-networkd-wait-online.service is enabled by default,
  # but we need to point it to the correct interface:
  # TODO(nit): is this really required? it used to be on flatcar.
  systemd.network.wait-online.extraArgs = [ "--interface=enp9s0" ];

  systemd.network.networks."10-enp" = {
    matchConfig.Name = "e*"; # enp9s0 (10G) or enp8s0 (1G)
    networkConfig = {
      IPv6AcceptRA = true;
      # On other machines:
      # DHCP = "yes";
      # But for storage2, we want a static config:
      Address = "10.0.0.252/24";
      Gateway = "10.0.0.1";
      DNS = "10.0.0.1";
    };
    ipv6AcceptRAConfig = {
      Token = "::10:0:0:252";
    };
  };

  services.tailscale.enable = true;

  services.prometheus.exporters.node = {
    enable = true;
    listenAddress = "storage2.example.ts.net";
  };
  systemd.services."prometheus-node-exporter" = {
    # https://michael.stapelberg.ch/posts/2024-01-17-systemd-indefinite-service-restarts/
    startLimitIntervalSec = 0;
    serviceConfig = {
      Restart = "always";
      RestartSec = 1;
    };
  };

  # Copy the NixOS configuration file and link it from the resulting system
  # (/run/current-system/configuration.nix). This is useful in case you
  # accidentally delete configuration.nix.
  # system.copySystemConfiguration = true;

  # This option defines the first version of NixOS you have installed on this particular machine,
  # and is used to maintain compatibility with application data (e.g. databases) created on older NixOS versions.
  #
  # Most users should NEVER change this value after the initial install, for any reason,
  # even if you've upgraded your system to a new NixOS release.
  system.stateVersion = "24.11"; # NEVER CHANGE; this is not an upgrade!
}

{
  config,
  lib,
  pkgs,
  ...
}:

let
  # Pick up sudo from /run/wrappers/bin
  wrappedSystemctl = pkgs.writeShellScriptBin "systemctl" ''
    exec sudo ${pkgs.systemd}/bin/systemctl "$@"
  '';

  wrappedPath = pkgs.buildEnv {
    name = "dramaqueen-env";
    paths = [
      wrappedSystemctl
      pkgs.samba
    ];
  };
in
{
  nixpkgs.overlays = [
    (final: prev: {
      dramaqueen = import ./dramaqueenPkg.nix {
        pkgs = final;
      };
    })
  ];

  users.users.dramaqueen = {
    isSystemUser = true;
    group = "dramaqueen";
  };

  users.groups.dramaqueen = { };

  security.sudo.extraRules = [
    {
      users = [ "dramaqueen" ];
      commands = [
        {
          command = "${pkgs.systemd}/bin/systemctl poweroff";
          options = [ "NOPASSWD" ];
        }
      ];
    }
  ];

  systemd.services.dramaqueen = {
    # https://michael.stapelberg.ch/posts/2024-01-17-systemd-indefinite-service-restarts/
    startLimitIntervalSec = 0;
    description = "dramaqueen";
    documentation = [ "https://github.com/stapelberg/zkj-nas-tools" ];
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Restart = "always";
      RestartSec = 1;

      # Cannot use DynamicUser= because we need sudo rules.
      User = "dramaqueen";
      Group = "dramaqueen";

      Environment = [
        "PATH=${wrappedPath}/bin:${pkgs.samba}/bin:/run/wrappers/bin:/usr/bin:/bin"
      ];

      ExecStart = "${pkgs.dramaqueen}/bin/dramaqueen";
    };
  };

  networking.firewall.allowedTCPPorts = [ 4414 ];
}

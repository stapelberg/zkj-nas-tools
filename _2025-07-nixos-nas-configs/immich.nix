{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:

{
  services.immich = {
    enable = true;
    host = "10.0.0.252";
    port = 2283;
    openFirewall = true;
    mediaLocation = "/srv/immich";
  };
  systemd.services."immich-server" = {
    unitConfig.RequiresMountsFor = [ "/srv" ];
    wantedBy = [ "srv.mount" ];
  };
}

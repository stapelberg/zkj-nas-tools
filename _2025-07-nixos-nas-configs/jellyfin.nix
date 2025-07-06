{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:

{
  services.jellyfin = {
    enable = true;
    openFirewall = true;
  };
  systemd.services.jellyfin = {
    unitConfig.RequiresMountsFor = [ "/srv" ];
    wantedBy = [ "srv.mount" ];
  };

}

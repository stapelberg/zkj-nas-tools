{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:

{
  services.samba = {
    enable = true;
    openFirewall = true;
    settings = {
      "global" = {
        "map to guest" = "bad user";
        "interfaces" = "10.0.0.252";
        "bind interfaces only" = "yes";
      };
      "data" = {
        "path" = "/srv/data";
        "comment" = "public data";
        "read only" = "no";
        "create mask" = "0775";
        "directory mask" = "0775";
        "guest ok" = "yes";
      };
    };
  };
  system.activationScripts.samba_user_create = ''
    smb_password="secret"
    echo -e "$smb_password\n$smb_password\n" | ${lib.getExe' pkgs.samba "smbpasswd"} -a -s michael
  '';

}

{
  config,
  lib,
  pkgs,
  modulesPath,
  ...
}:

{
  environment.etc."ssl/certs/r.zekjur.net.crt".text = ''
    -----BEGIN CERTIFICATE-----
    MIID8TCCAlmgAwIBAgIRAPWwvYWpoH+lGKv6rxZvC4MwDQYJKoZIhvcNAQELBQAw
    […]
    -----END CERTIFICATE-----
  '';

  systemd.services.unlock = {
    wantedBy = [ "multi-user.target" ];
    description = "unlock hard drive";
    wants = [ "network.target" ];
    after = [ "systemd-networkd-wait-online.service" ];
    before = [ "samba.service" ]; # TODO: seems unnecessary?
    serviceConfig = {
      Type = "oneshot";
      RemainAfterExit = "yes";
      ExecStart = [
        # Wait until the host is actually reachable.
        ''/bin/sh -c "c=0; while [ $c -lt 5 ]; do ${pkgs.iputils}/bin/ping -n -c 1 r.zekjur.net && break; c=$((c+1)); sleep 1; done"''

        ''/bin/sh -c "[ -e \"/dev/mapper/S5SSNF0T205183F_crypt\" ] || (echo -n my_local_secret && ${pkgs.wget}/bin/wget --retry-connrefused --ca-directory=/dev/null --ca-certificate=/etc/ssl/certs/r.zekjur.net.crt -qO - https://r.zekjur.net:8443/sdb2_crypt) | ${pkgs.cryptsetup}/bin/cryptsetup --key-file=- luksOpen /dev/disk/by-id/ata-Samsung_SSD_870_QVO_8TB_S5SSNF0T205183F S5SSNF0T205183F_crypt"''

        ''/bin/sh -c "[ -e \"/dev/mapper/S5SSNJ0T205991B_crypt\" ] || (echo -n my_local_secret && ${pkgs.wget}/bin/wget --retry-connrefused --ca-directory=/dev/null --ca-certificate=/etc/ssl/certs/r.net.crt -qO - https://r.zekjur.net:8443/sdb2_crypt) | ${pkgs.cryptsetup}/bin/cryptsetup --key-file=- luksOpen /dev/disk/by-id/ata-Samsung_SSD_870_QVO_8TB_S5SSNJ0T205991B S5SSNJ0T205991B_crypt"''

        ''/bin/sh -c "${pkgs.lvm2.bin}/bin/vgchange -ay"''
        # Let systemd mount /srv based on the fileSystems./srv
        # declaration to prevent race conditions: mount
        # might not succeed while the fsck is still in progress,
        # for example, which otherwise makes unlock.service fail.
      ];
    };

  };

  # Signal readiness on HTTP port 8200 once /srv is mounted:
  networking.firewall.allowedTCPPorts = [ 8200 ];
  services.caddy = {
    enable = true;
    virtualHosts."http://10.0.0.252:8200".extraConfig = ''
      respond "ok"
    '';
  };
  systemd.services.caddy = {
    unitConfig.RequiresMountsFor = [ "/srv" ];
    wantedBy = [ "srv.mount" ];
  };

  fileSystems."/srv" = {
    device = "/dev/mapper/data-data";
    fsType = "ext4";
    options = [
      "nofail"
      "x-systemd.requires=unlock.service"
    ];
  };
}

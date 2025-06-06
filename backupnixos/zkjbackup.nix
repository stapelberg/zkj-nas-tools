{ config, lib, pkgs, ... }:

# For string literal escaping rules (''${), see:
# https://nix.dev/manual/nix/2.26/language/string-literals#string-literals

# For writers.writePerlBin, see:
# https://nixos.wiki/wiki/Nix-writers

# For lib.concatStringsSep, see:
# https://nix.dev/manual/nix/2.18/language/builtins#builtins-concatStringsSep

let
  backuppl = pkgs.writers.writePerlBin "backuppl" { libraries = []; } ''
# This script is run via ssh from dornröschen.
use strict;
use warnings;
use POSIX qw(strftime);

if (my ($destination) = ($ENV{SSH_ORIGINAL_COMMAND} =~ /^([a-z0-9.]+)$/)) {
    $destination =~ s/\./:/g;
    $destination = "[2a02:168:4a00:0:''${destination}]";
    # Figure out the last backup time:
    my $lines = `${pkgs.rsync}/bin/rsync --list-only -e ssh ''${destination}:/`;
    my @dates = sort
                grep { defined }
                map { / ([0-9-]+)$/ && $1 || undef }
                split("\n", $lines);
    my $today = strftime("%Y-%m-%d", localtime);

# services.zkjbackup.preBackupHooks
${lib.concatStringsSep "\n" (map (p: "{\nmy @hook = (\"${p}\");\nsystem(@hook) == 0 or die \"hook failed: \?\";\n}\n") config.services.zkjbackup.preBackupHooks)}

    my @rsync = (
        "${pkgs.rsync}/bin/rsync",
        "--stats",
        "-e",
        "ssh",
        "-ax",
        "--numeric-ids",
        "--relative",
# services.zkjbackup.excludePaths:
${lib.concatStringsSep "\n" (map (p: "\"--exclude\", \"${p}\",") config.services.zkjbackup.excludePaths)}
        "/",
        "''${destination}:/$today",
    );
    if (@dates > 0) {
        my $last_backup = $dates[@dates-1];
        # In case a backup already ran today, chances are it was run
        # manually, so make this run just a noop.
        exit 0 if $last_backup eq $today;
        push @rsync, "--link-dest=/$last_backup";
    }
    exec @rsync;
} else {
    print STDERR "Could not parse SSH_ORIGINAL_COMMAND.\n";
}
'';
in
{
  options.services.zkjbackup.excludePaths = lib.mkOption {
    type = with lib.types; listOf str;
    default = [];
    description = "List of paths to exclude from the backup.";
  };

  options.services.zkjbackup.preBackupHooks = lib.mkOption {
    type = with lib.types; listOf str;
    default = [];
    description = "List of commands to run before the backup.";
  };

  config.users.users.root.openssh.authorizedKeys.keys = lib.mkAfter [
    ''command="${backuppl}/bin/backuppl",no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICh4nMgU3AneqUonvplI/Nx1LrSCG5C6eM4LKKZW+Yxn michael@dr.zekjur.net''
  ];

  config.programs.ssh.knownHosts = lib.mkAfter {
    "storage2-rsa" = {
      hostNames = [ "10.0.0.252" "2a02:168:4a00:0:10::252" ];
      publicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCxbdL6enfSoD9Uv++PxpUaM6bh0GOW4pdAR5qQwnChXrGTuyD1AdX9ZoJT3Ko9tucUAlIOQST5Ztwu9EJp42qpzxJRptdfCZThONvIQ8Kjry/fbyEAVkPd4JT+bVXnnavcTKo68+Oh0LTWcQnM4UN6QqyF0DS/ywWron6m9u/T6uNzZNqCmMdKzZOVw1Ajv4hXOq0sqF2A3fxuq+usZg6w16Njl41O8do5t61ru3yzJ/tfDbmyPzXmTgCpR0ng0frR/rmnuFtkrL3jmHhINeApgAgNRsWmDw2289nwdaQqmB1GFV9Bgi12NbLFo6457zaNu2vbcQ/df24Qds4jwudYmpsXcTl1HMGHtvNZNvn0oC0QGY+nPYr8mm0MkJqv836E6nF0JA1AnPqBCZyoUtm4LThIuKuM2v+jKZdlynN1F9/ZFp1MrV5/H/SXBvOTl08SJbvz2pgi/wPNVKSW1JozNznaLq0ignfuRNDxoTIidJCsTvRIAJchoVE/eZcNFvE=";
    };

    "storage2-ed25519" = {
      hostNames = [ "10.0.0.252" "2a02:168:4a00:0:10::252" ];
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILAyZ83Uxkk1C8z8AdhK2hc4/rtdwZwIOgVMnAspCMkJ";
    };

    "storage3-ed25519" = {
      hostNames = [ "10.0.0.253" "2a02:168:4a00:0:10::253" ];
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP833ofUfclrHj9n3YpqPfMN6CTlbhe+/yEj76hxal8k";
    };

    "storage3-rsa" = {
      hostNames = [ "10.0.0.253" "2a02:168:4a00:0:10::253" ];
      publicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCHcTgcgspjcKtj92nbnhJklbkC4J4rSJAQ6KG+deLMJDVSxSw8HZAHXx4eJPA0+QMmY6gmOcwshAevRXbXObcOhKZduD17+oRlRm2pLHWSirKjB3EBvT0jYrMrf0CZQ+uxU7ozVeyPspoOAxNr7ekqJDMYfM+/xdfysre9YQnxt6dfDF425WdjlnJ+aeXRzIDOaAZmnFuMqUfSlqYyh3Tfg6gwVTkiMMwlEcMiNmIofAEa2oMMaGL6Qi6OzreH/sK7Jz8n5p2Bezk88D53TB87VyLkAj/eUS8J+ptDuhOgKfOvxIR2iNwDQN7gcDx/JurxZd9wP6NSVEAe45ddPRefn9uDHnK6g9ClbuHpB8AnDu8uyT4Gbcx0gKqFE+MpSKEZQoUEARYN7LmF6GuoWNC3pHol5QmUALt2isDTcwAvZ6rnFm3RUFfQZPKy/dAEpzRPbeAqBdekSoLfhR/D7S/X9M5ol+wFbhHrdolibUy67gG7pfd16zD9u33bZ4BzNfs=";
    };

    "storage3-ecdsa" = {
      hostNames = [ "10.0.0.253" "2a02:168:4a00:0:10::253" ];
      publicKey = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBChEmC7cqFIiJvlQDcudCuYeR5k3Km8nBAos20evrylj3LFqk15zjiARcygQF8iDxqP/HC8YELAJqG7EiLmHChQ=";
    };
  };

}

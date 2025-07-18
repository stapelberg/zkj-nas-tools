{
  config,
  lib,
  pkgs,
  ...
}:

# For string literal escaping rules (''${), see:
# https://nix.dev/manual/nix/2.26/language/string-literals#string-literals

# For writers.writePerlBin, see:
# https://wiki.nixos.org/wiki/Nix-writers

# For lib.concatStringsSep, see:
# https://nix.dev/manual/nix/2.18/language/builtins#builtins-concatStringsSep

let
  postgresDump = pkgs.writers.writePerlBin "zkj-postgres-dump" { libraries = [ ]; } ''
    use strict;
    use warnings;
    use POSIX qw(strftime);
    use Data::Dumper;

    # User postgres has no permission to work in /root:
    chdir("/");

    my $today = strftime("%Y-%m-%d", localtime);

    # Dump globals (not included in pg_dump): https://stackoverflow.com/a/16619878/712014

    my @dumpall = (
        "${pkgs.sudo}/bin/sudo",
        "-u",
        "postgres",
        "sh",
        "-c",
        "mkdir -p /var/pgdump/$today/ && ${pkgs.postgresql}/bin/pg_dumpall --globals-only > /var/pgdump/$today/globals.sql"
    );
    system(@dumpall) == 0
        or die "pg_dumpall failed: $?";

    for my $db (@ARGV) {
      my @dump;
      # Run pg_dump for all databases:
      @dump = (
          "${pkgs.sudo}/bin/sudo",
          "-u",
          "postgres",
          "sh",
          "-c",
          "mkdir -p /var/pgdump/$today/ && ${pkgs.postgresql}/bin/pg_dump -Fc $db > /var/pgdump/$today/$db.custom"
      );
      system(@dump) == 0
          or die "pg_dump failed: $?";
    }

    # Prune older dumps:
    my @prune = (
        "${pkgs.findutils}/bin/find",
        "/var/pgdump",
        "-mindepth",
        "1",
        "-not",
        "-path",
        "/var/pgdump/''${today}*",
        "-delete",
       );
    print "prune: " . Dumper(\@prune) . "\n";
    system(@prune) == 0
        or die "prune failed: $?";
  '';

  postgresDumpWrapper = pkgs.writeShellScriptBin "zkj-postgres-dump" ''
    exec ${postgresDump}/bin/zkj-postgres-dump ${lib.escapeShellArgs config.services.zkjbackup.postgresqlDatabases}
  '';

  backuppl = pkgs.writers.writePerlBin "backuppl" { libraries = [ ]; } ''
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
    ${lib.concatStringsSep "\n" (
      map (
        p: "{\nmy @hook = (\"${p}\");\nsystem(@hook) == 0 or die \"hook failed: \?\";\n}\n"
      ) config.services.zkjbackup.preBackupHooks
    )}

        my @rsync = (
            "${pkgs.rsync}/bin/rsync",
            "--stats",
            "-e",
            "ssh",
            "-ax",
            "--numeric-ids",
            "--relative",
    # services.zkjbackup.excludePaths:
    ${lib.concatStringsSep "\n" (
      map (p: "\"--exclude\", \"${p}\",") config.services.zkjbackup.excludePaths
    )}
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
    default = [ ];
    description = "List of paths to exclude from the backup.";
  };

  options.services.zkjbackup.postgresqlDatabases = lib.mkOption {
    type = with lib.types; listOf str;
    default = [ ];
    description = "Names of Postgresql databases to dump.";
  };

  options.services.zkjbackup.preBackupHooks = lib.mkOption {
    type = with lib.types; listOf str;
    default =
      let
        dbs = config.services.zkjbackup.postgresqlDatabases;
      in
      if dbs != [ ] then
        [
          "${postgresDump}/bin/zkj-postgres-dump ${lib.concatStringsSep " " dbs}"
        ]
      else
        [ ];
    description = "List of commands to run before the backup.";
  };

  # Install zkj-postgres-dump as a command to run interactively,
  # for pull-prod scripts for development.
  config.environment.systemPackages = [
    postgresDumpWrapper
  ];

  config.users.users.root.openssh.authorizedKeys.keys = lib.mkAfter [
    ''command="${backuppl}/bin/backuppl",no-port-forwarding,no-X11-forwarding ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICh4nMgU3AneqUonvplI/Nx1LrSCG5C6eM4LKKZW+Yxn michael@dr.zekjur.net''
  ];

  config.programs.ssh.knownHosts = lib.mkAfter {
    "storage2-rsa" = {
      hostNames = [
        "10.0.0.252"
        "2a02:168:4a00:0:10::252"
      ];
      publicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCxbdL6enfSoD9Uv++PxpUaM6bh0GOW4pdAR5qQwnChXrGTuyD1AdX9ZoJT3Ko9tucUAlIOQST5Ztwu9EJp42qpzxJRptdfCZThONvIQ8Kjry/fbyEAVkPd4JT+bVXnnavcTKo68+Oh0LTWcQnM4UN6QqyF0DS/ywWron6m9u/T6uNzZNqCmMdKzZOVw1Ajv4hXOq0sqF2A3fxuq+usZg6w16Njl41O8do5t61ru3yzJ/tfDbmyPzXmTgCpR0ng0frR/rmnuFtkrL3jmHhINeApgAgNRsWmDw2289nwdaQqmB1GFV9Bgi12NbLFo6457zaNu2vbcQ/df24Qds4jwudYmpsXcTl1HMGHtvNZNvn0oC0QGY+nPYr8mm0MkJqv836E6nF0JA1AnPqBCZyoUtm4LThIuKuM2v+jKZdlynN1F9/ZFp1MrV5/H/SXBvOTl08SJbvz2pgi/wPNVKSW1JozNznaLq0ignfuRNDxoTIidJCsTvRIAJchoVE/eZcNFvE=";
    };

    "storage2-ed25519" = {
      hostNames = [
        "10.0.0.252"
        "2a02:168:4a00:0:10::252"
      ];
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILAyZ83Uxkk1C8z8AdhK2hc4/rtdwZwIOgVMnAspCMkJ";
    };

    "storage3-ed25519" = {
      hostNames = [
        "10.0.0.253"
        "2a02:168:4a00:0:10::253"
      ];
      publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINHmcUsv4xYAu6oBCoP5904xqx8X1Qi6W1XNRKBK3AF8";
    };

    "storage3-rsa" = {
      hostNames = [
        "10.0.0.253"
        "2a02:168:4a00:0:10::253"
      ];
      publicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDS+R0Jfq1ybrb0fQYXsyKYaJEbxgl8yFRK4J+uPTkqUoqcbLNwzbLC4/1KWLbzRxexltCmkFb3P9QonckXBUE0SIs9zwhlnRhquQwF1adyQ4veN69PiZ5eNHdeT0R1fyimPMdBlutOtnupunM23EVfUyn7dusuyl3WM+aASjnMgHHamd+9RykCW5UlJH21AuT6tJc1Gt2qa6FMlusZcbYP9WZ0gDSCK9dig01+p7Tfn0OQpWExhiRJRr/tiRkeHUtQCIrZaMffZgy/0j6wcNVqF2cG1dp4NCvn1LFuI9TWVOriwFC03iukJ/SNyMpjIu+yu1lAL/wJrH3eld66/dAHz3In/hU8okSS9sE3Unii6HfHBzeCvfkzxLCif6n+23ZxHuVCrsKyVuOhlPLhF7bZvpxNaCEOvRzSzc0w9Z9AE8V5KESb9KBn8Jq7UlwwaOQdVL1CJEDhJtlIYkJ8Jfk6myPIA/UisuBwR0gyaR0uG5KrT7GSMIVjPmR0jave2ftLxwxn5LtmbOiWnTWawWPod8u0WEqK8g5AuMLX4TzX+9dEt+u0eLGXIKIH6A26VXgpQkphnz7EbIaWtT5RVssAGK0PmA3T6WuOfUJj1OmKeL+OBv2vVC0M4tc2xVjyC7hoLW6+QKpgxwrY1sqBQv0Fmh+RvM8eRrORQdHdHaJN2Q==";
    };
  };
}

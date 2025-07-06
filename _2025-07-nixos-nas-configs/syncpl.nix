{ pkgs }:

# For string literal escaping rules (''${), see:
# https://nix.dev/manual/nix/2.26/language/string-literals#string-literals

# For writers.writePerlBin, see https://wiki.nixos.org/wiki/Nix-writers

pkgs.writers.writePerlBin "syncpl" { libraries = [ ]; } ''
  # This script is run via ssh from dornröschen.
  use strict;
  use warnings;
  use Data::Dumper;

  if (my ($destination) = ($ENV{SSH_ORIGINAL_COMMAND} =~ /^([a-z0-9.]+)$/)) {
      print STDERR "rsync version: " . `${pkgs.rsync}/bin/rsync --version` . "\n\n";
      my @rsync = (
          "${pkgs.rsync}/bin/rsync",
          "-e",
          "ssh",
          "--max-delete=-1",
          "--verbose",
          "--stats",
          # Intentionally not setting -X for my data sync,
          # where there are no full system backups; mostly media files.
          "-ax",
          "--ignore-existing",
          "--omit-dir-times",
          "/srv/data/",
          "''${destination}:/",
      );
      print STDERR "running: " . Dumper(\@rsync) . "\n";
      exec @rsync;
  } else {
      print STDERR "Could not parse SSH_ORIGINAL_COMMAND.\n";
  }
''

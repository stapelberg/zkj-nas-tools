#!/usr/bin/env perl
# This script is run via ssh from dornröschen.
use strict;
use warnings;

if (my ($destination) = ($ENV{SSH_ORIGINAL_COMMAND} =~ /^([a-z0-9.]+)$/)) {
    my @rsync = (
        "/usr/bin/rsync",
        "--max-delete=-1",
        "--verbose",
        "--stats",
        "-aXx",
        "--ignore-existing",
        "--omit-dir-times",
        "/srv/data/",
        "${destination}::data",
    );

    exec @rsync;
} else {
    print STDERR "Could not parse SSH_ORIGINAL_COMMAND.\n";
}

#!/usr/bin/env perl
# This script is run via ssh from dornröschen.
use strict;
use warnings;
use POSIX qw(strftime);

if (my ($destination) = ($ENV{SSH_ORIGINAL_COMMAND} =~ /^([a-z0-9.]+)$/)) {
    # Figure out the last backup time:
    my $lines = `/usr/bin/rsync --list-only ${destination}::midna`;
    my @dates = sort
                grep { defined }
                map { / ([0-9-]+)$/; $1 }
                split("\n", $lines);
    my $today = strftime("%Y-%m-%d", localtime);
    my @rsync = (
        "/usr/bin/rsync",
        "-aXx",
        "--numeric-ids",
        "--relative",
        "/",
        "/boot",
        "rsync://$destination/midna/$today",
    );
    if (@dates > 0) {
        my $last_backup = $dates[@dates-1];
        # In case a backup already ran today, chances are it was run
        # manually, so make this run just a noop.
        exit 0 if $last_backup eq $today;
        push @rsync, "--link-dest=../$last_backup";
    }
    exec @rsync;
} else {
    print STDERR "Could not parse SSH_ORIGINAL_COMMAND.\n";
}

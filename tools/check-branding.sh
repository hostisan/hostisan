#!/usr/bin/env bash
# check-branding.sh ROOT: fail when user-visible core text under ROOT still says
# Hestia (Plan 6 B3d). ROOT is a source tree or an installed /usr/local/hestia.
#
# Scanned: func/ bin/ install/common/templates/email/ web/images/ web/error/
# src/deb/*/control and ui/dist/ (whichever exist). Each occurrence of
# "Hestia", "Hestia Control Panel", "HestiaCP" or "hestiacp.com" is reported as
# PATH:LINE: TEXT unless
#   - its line is a comment (starts with #, //, * or <!--), or
#   - an entry of tools/branding-allow.txt covers it (see that file).
# Exit 0 when nothing is reported, 1 when something is, 2 on a usage error.
set -euo pipefail

root=${1:-}
if [ -z "$root" ] || [ ! -d "$root" ]; then
	echo "usage: check-branding.sh ROOT (an existing directory)" >&2
	exit 2
fi
allow=${BRANDING_ALLOW:-$(cd "$(dirname "$0")" && pwd)/branding-allow.txt}
if [ ! -f "$allow" ]; then
	echo "check-branding.sh: allow list $allow not found" >&2
	exit 2
fi

cd "$root"
paths=()
for p in func bin install/common/templates/email web/images web/error src/deb/*/control ui/dist; do
	[ -e "$p" ] && paths+=("$p")
done
[ ${#paths[@]} -eq 0 ] && exit 0

find "${paths[@]}" -type f -print0 | perl -0 -e '
	use strict;
	use warnings;
	my $allowfile = shift;
	my @allow;
	open(my $af, "<", $allowfile) or die "cannot read $allowfile: $!\n";
	my @lines = do { local $/ = "\n"; <$af> };
	close $af;
	for my $l (@lines) {
		$l =~ s/\r?\n\z//;
		next if $l =~ /^\s*(?:#.*)?\z/;
		my ($path, $text) = split /\s+/, $l, 2;
		die "bad allow entry (want PATH_RE TEXT_RE): $l\n" unless defined $text && length $text;
		push @allow, [qr/^(?:$path)\z/, qr/$text/];
	}
	my $brand = qr/HestiaCP|Hestia(?: Control Panel)?|hestiacp\.com/;
	my $bad = 0;
	local $/ = "\0";
	while (my $file = <STDIN>) {
		chomp $file;
		next if -B $file;
		my @rules = grep { $file =~ $_->[0] } @allow;
		open(my $fh, "<", $file) or die "cannot read $file: $!\n";
		local $/ = "\n";
		while (my $line = <$fh>) {
			next if $line =~ m{^\s*(#|//|\*|<!--)};
			chomp $line;
			while ($line =~ /$brand/g) {
				my ($s, $e) = ($-[0], $+[0]);
				my $ok = 0;
				RULE: for my $r (@rules) {
					my $copy = $line;
					while ($copy =~ /$r->[1]/g) {
						if ($-[0] <= $s && $+[0] >= $e) { $ok = 1; last RULE; }
					}
				}
				pos($line) = $e;
				next if $ok;
				my $from = $s > 60 ? $s - 60 : 0;
				printf "%s:%d: %s\n", $file, $., substr($line, $from, $e - $from + 60);
				$bad++;
			}
		}
		close $fh;
	}
	exit($bad ? 1 : 0);
' "$allow"

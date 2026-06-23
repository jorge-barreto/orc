#!/bin/sh
# version-guard.sh — release-version guards for the auto-tag workflow.
#
#   version-guard.sh validate <version>
#       Exit 0 if <version> is strict semver MAJOR.MINOR.PATCH (digits only).
#
#   version-guard.sh newer <candidate> <latest>
#       Exit 0 if <candidate> is strictly greater than <latest> by semver
#       compare. <latest> may be empty (no prior release) — any valid
#       <candidate> is then "newer". Both args are plain semver (no leading v).
#
# All versions are the GoReleaser/VERSION-file form: no leading "v".

set -eu

err() {
	printf 'version-guard: %s\n' "$1" >&2
	exit 1
}

is_semver() {
	# Strict MAJOR.MINOR.PATCH, digits only, no pre-release/build metadata.
	printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
}

cmd="${1:-}"
case "$cmd" in
	validate)
		v="${2:-}"
		[ -n "$v" ] || err "validate: missing version argument"
		is_semver "$v" || err "validate: '$v' is not strict semver MAJOR.MINOR.PATCH"
		;;
	newer)
		cand="${2:-}"
		latest="${3:-}"
		[ -n "$cand" ] || err "newer: missing candidate argument"
		is_semver "$cand" || err "newer: candidate '$cand' is not strict semver"
		# No prior release: any valid candidate is newer.
		[ -n "$latest" ] || exit 0
		is_semver "$latest" || err "newer: latest '$latest' is not strict semver"
		# Compare component by component.
		cand_major=$(printf '%s' "$cand" | cut -d. -f1)
		cand_minor=$(printf '%s' "$cand" | cut -d. -f2)
		cand_patch=$(printf '%s' "$cand" | cut -d. -f3)
		lat_major=$(printf '%s' "$latest" | cut -d. -f1)
		lat_minor=$(printf '%s' "$latest" | cut -d. -f2)
		lat_patch=$(printf '%s' "$latest" | cut -d. -f3)
		if [ "$cand_major" -gt "$lat_major" ]; then exit 0; fi
		if [ "$cand_major" -lt "$lat_major" ]; then err "candidate $cand <= latest $latest"; fi
		if [ "$cand_minor" -gt "$lat_minor" ]; then exit 0; fi
		if [ "$cand_minor" -lt "$lat_minor" ]; then err "candidate $cand <= latest $latest"; fi
		if [ "$cand_patch" -gt "$lat_patch" ]; then exit 0; fi
		err "candidate $cand <= latest $latest"
		;;
	*)
		err "usage: version-guard.sh {validate <version> | newer <candidate> <latest>}"
		;;
esac

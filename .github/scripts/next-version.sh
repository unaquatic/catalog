#!/usr/bin/env bash
# next-version.sh prints the semantic version a commit releases, from the
# last version and the commit's message, as Conventional Commits types it:
# a breaking change, as "feat!:" or a "BREAKING CHANGE:" footer, bumps the
# major version, or the minor one while the major is 0; feat bumps the
# minor version; anything else the patch.
#
#   next-version.sh LAST_VERSION < commit-message
#
# LAST_VERSION is vX.Y.Z, or empty for none, which makes v0.1.0.
set -euo pipefail

last=${1:-}
msg=$(cat)
subject=$(head -n 1 <<< "$msg")

if [ -z "$last" ]; then
	echo v0.1.0
	exit 0
fi
if ! [[ $last =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	echo "next-version.sh: $last is no version vX.Y.Z" >&2
	exit 1
fi
major=${BASH_REMATCH[1]} minor=${BASH_REMATCH[2]} patch=${BASH_REMATCH[3]}

# The patterns are variables, so that bash reads their parentheses as
# regular expression, not as its own syntax.
breaking_re='^[a-z]+(\([^)]*\))?!:'
feat_re='^feat(\([^)]*\))?:'
if [[ $subject =~ $breaking_re ]] || grep -qE '^BREAKING[ -]CHANGE:' <<< "$msg"; then
	if [ "$major" -eq 0 ]; then
		minor=$((minor + 1)) patch=0
	else
		major=$((major + 1)) minor=0 patch=0
	fi
elif [[ $subject =~ $feat_re ]]; then
	minor=$((minor + 1)) patch=0
else
	patch=$((patch + 1))
fi
echo "v$major.$minor.$patch"

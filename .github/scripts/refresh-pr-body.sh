#!/usr/bin/env bash
# refresh-pr-body.sh writes the body of a refresh pull request, in the
# shape of the repository's pull requests: Summary, Verification and Notes
# for reviewers.
#
#   refresh-pr-body.sh PREVIOUS_CATALOG PATCH CATALOG > body.md
#
# PREVIOUS_CATALOG and CATALOG are the catalog before and after go generate
# ./data/, each joined into one catalog.json, as catalog join writes it;
# PATCH is data/patch.json after it.
set -euo pipefail

prev=$1
patch=$2
catalog=$3

# count OP prints how many of the patch's operations are OP.
count() { jq --arg op "$1" '[.ops[] | select(.op == $op)] | length' "$patch"; }

# ids KIND OP prints the IDs of the models or providers the patch adds or
# removes whole, one per line, as the catalog keys them.
ids() {
	jq -r --arg kind "$1" --arg op "$2" '.ops[] | select(.op == $op)
		| .path | select(test("^/" + $kind + "/[^/]+$"))
		| sub("^/" + $kind + "/"; "") | gsub("~1"; "/") | gsub("~0"; "~")' "$patch"
}

# list TITLE prints the IDs on stdin as a bullet, up to 30 of them.
list() {
	local all n
	all=$(cat)
	[ -n "$all" ] || return 0
	n=$(grep -c . <<< "$all")
	printf -- '- %s (%d): ' "$1" "$n"
	head -30 <<< "$all" | sed 's/.*/`&`/' | paste -sd ',' - | sed 's/,/, /g' | tr -d '\n'
	[ "$n" -le 30 ] || printf ', and %d more' $((n - 30))
	printf '.\n'
}

conflicts=$(jq '.conflicts // [] | length' "$patch")
models_before=$(jq '.models | length' "$prev")
models_after=$(jq '.models | length' "$catalog")
providers_before=$(jq '.providers | length' "$prev")
providers_after=$(jq '.providers | length' "$catalog")

echo "## Summary"
echo
echo "- Apply models.dev's changes since \`data/models-dev.json\` to \`data/catalog\`: $(count add) added, $(count replace) replaced, $(count remove) removed."
if [ "$conflicts" -gt 0 ]; then
	noun=conflicts
	[ "$conflicts" -ne 1 ] || noun=conflict
	echo "- Keep our own value at $conflicts $noun, where we changed what models.dev changed too; \`data/patch.json\` lists each with models.dev's old and new values and ours."
fi
ids models add | list "Add models"
ids models remove | list "Remove models, which models.dev dropped"
ids providers add | list "Add providers"
ids providers remove | list "Remove providers, which models.dev dropped"
echo
echo "## Verification"
echo
echo "- \`go generate ./data/\` (applied the patch)"
echo "- \`go test ./...\` (passed)"
echo "- Sanity check: $models_before → $models_after models, $providers_before → $providers_after providers (passed)"
echo
echo "## Notes for reviewers"
echo
if [ "$conflicts" -gt 0 ]; then
	echo "- Review the conflicts in \`data/patch.json\` first. Ours was kept at each; to take models.dev's value, edit \`data/catalog\` on \`main\`."
else
	echo "- No conflicts: no value models.dev changed was one we changed too."
fi
echo "- The next refresh rebuilds this branch from \`main\` and force-pushes it, so edits pushed here are lost; make them on \`main\`, where the next refresh keeps them."
echo "- Merging releases and deploys the catalog."

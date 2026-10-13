#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "Usage: sh apply-defaults.sh /path/to/infinite-canvas" >&2
    exit 1
fi

canvas_dir=$(CDPATH= cd "$1" && pwd)
canvas_dir=$(git -C "$canvas_dir" rev-parse --show-toplevel)
integration_dir=$(CDPATH= cd "$(dirname "$0")" && pwd)
expected_revision=dab19adc0847e32e39b7fc8ff90cb392561fb826
actual_revision=$(git -C "$canvas_dir" rev-parse HEAD)

if [ "$actual_revision" != "$expected_revision" ]; then
    echo "Expected canvas revision $expected_revision; found $actual_revision." >&2
    exit 1
fi

seedance_patch="$integration_dir/seedance.patch"
subpath_patch="$integration_dir/subpath.patch"

# The three patches stack: seedance.patch edits files defaults.patch already touched,
# subpath.patch comes last. So "is it applied?" can only be asked of the last patch in
# the stack. Check that first, then require a pristine tree at the pinned revision —
# a half-applied tree cannot be detected by reverse-checking the earlier patches and
# would otherwise fail deep inside `git apply` with unreadable messages.
if git -C "$canvas_dir" apply --reverse --check "$subpath_patch" 2>/dev/null; then
    echo "Gateway defaults, Seedance integration and sub-path support are already applied."
    exit 0
fi

if [ -n "$(git -C "$canvas_dir" status --porcelain)" ]; then
    echo "$canvas_dir is not clean at $expected_revision." >&2
    echo "Reset it first (git -C $canvas_dir checkout -- . && git -C $canvas_dir clean -fd)" >&2
    echo "and rerun this script from the pristine revision." >&2
    exit 1
fi

patch_file="$integration_dir/defaults.patch"
git -C "$canvas_dir" apply --check "$patch_file"
git -C "$canvas_dir" apply "$patch_file"
git -C "$canvas_dir" apply --check "$seedance_patch"
git -C "$canvas_dir" apply "$seedance_patch"
git -C "$canvas_dir" apply --check "$subpath_patch"
git -C "$canvas_dir" apply "$subpath_patch"
echo "Gateway defaults, Seedance integration and /canvas/ sub-path support applied. Rebuild the canvas before deployment."

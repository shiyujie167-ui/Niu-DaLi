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
if git -C "$canvas_dir" apply --reverse --check "$subpath_patch" 2>/dev/null; then
    echo "Gateway defaults, Seedance integration and sub-path support are already applied."
    exit 0
fi

patch_file="$integration_dir/defaults.patch"
if ! git -C "$canvas_dir" apply --reverse --check "$patch_file" 2>/dev/null; then
    git -C "$canvas_dir" apply --check "$patch_file"
    git -C "$canvas_dir" apply "$patch_file"
fi
git -C "$canvas_dir" apply --check "$seedance_patch"
git -C "$canvas_dir" apply "$seedance_patch"
if ! git -C "$canvas_dir" apply --reverse --check "$subpath_patch" 2>/dev/null; then
    git -C "$canvas_dir" apply --check "$subpath_patch"
    git -C "$canvas_dir" apply "$subpath_patch"
fi
echo "Gateway defaults, Seedance integration and /canvas/ sub-path support applied. Rebuild the canvas before deployment."

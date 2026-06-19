#!/bin/sh

set -eu

source_dir=${1:-.}
output_dir=${2:-dist}

rm -rf "$output_dir"
mkdir -p "$output_dir"

copy_hashed_asset() {
    source_name=$1
    extension=${source_name##*.}
    base_name=${source_name%.*}
    hash=$(sha256sum "$source_dir/$source_name" | cut -c1-12)
    output_name="${base_name}.${hash}.${extension}"

    cp "$source_dir/$source_name" "$output_dir/$output_name"
    printf '%s' "$output_name"
}

styles_file=$(copy_hashed_asset styles.css)
search_file=$(copy_hashed_asset search.js)
health_file=$(copy_hashed_asset health.js)

sed \
    -e "s|/styles.css|/$styles_file|g" \
    -e "s|/search.js|/$search_file|g" \
    "$source_dir/index.html" > "$output_dir/index.html"

sed \
    -e "s|/styles.css|/$styles_file|g" \
    -e "s|/health.js|/$health_file|g" \
    "$source_dir/health.html" > "$output_dir/health.html"

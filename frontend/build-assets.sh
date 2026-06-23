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

    mkdir -p "$output_dir/$(dirname "$output_name")"
    cp "$source_dir/$source_name" "$output_dir/$output_name"
    printf '%s' "$output_name"
}

mkdir -p "$output_dir/js"
for module_path in "$source_dir"/js/*.js; do
    module_name=$(basename "$module_path")
    if [ "$module_name" != "app.js" ]; then
        cp "$module_path" "$output_dir/js/$module_name"
    fi
done

base_css=$(copy_hashed_asset css/base.css)
home_css=$(copy_hashed_asset css/home.css)
search_css=$(copy_hashed_asset css/search.css)
logs_css=$(copy_hashed_asset css/logs.css)
health_css=$(copy_hashed_asset css/health.css)
responsive_css=$(copy_hashed_asset css/responsive.css)
app_js=$(copy_hashed_asset js/app.js)

sed \
    -e "s|/css/base.css|/$base_css|g" \
    -e "s|/css/home.css|/$home_css|g" \
    -e "s|/css/search.css|/$search_css|g" \
    -e "s|/css/logs.css|/$logs_css|g" \
    -e "s|/css/health.css|/$health_css|g" \
    -e "s|/css/responsive.css|/$responsive_css|g" \
    -e "s|/js/app.js|/$app_js|g" \
    "$source_dir/index.html" > "$output_dir/index.html"

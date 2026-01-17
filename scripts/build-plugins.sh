#!/bin/bash
# Build modular plugins by concatenating src/_*.js files into plugin.js
#
# Files are sorted alphabetically - use numeric prefixes for ordering:
#   _00_helpers.js, _01_utils.js, ..., _99_plugin.js
#
# Usage: ./scripts/build-plugins.sh [plugin-dir]
#        ./scripts/build-plugins.sh                    # Build all plugins with src/
#        ./scripts/build-plugins.sh plugins/app/synchub # Build specific plugin

set -e

build_plugin() {
    local plugin_dir="$1"
    local src_dir="$plugin_dir/src"
    local output="$plugin_dir/plugin.js"

    if [ ! -d "$src_dir" ]; then
        return 0
    fi

    echo "Building: $plugin_dir"

    # Get version from collection.json or config.json
    local version="?"
    local name="unknown"
    if [ -f "$plugin_dir/collection.json" ]; then
        version=$(grep -o '"ver"[[:space:]]*:[[:space:]]*[0-9]*' "$plugin_dir/collection.json" | grep -o '[0-9]*' | head -1)
        name=$(grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' "$plugin_dir/collection.json" | sed 's/.*"\([^"]*\)"$/\1/' | head -1)
    elif [ -f "$plugin_dir/config.json" ]; then
        version=$(grep -o '"ver"[[:space:]]*:[[:space:]]*[0-9]*' "$plugin_dir/config.json" | grep -o '[0-9]*' | head -1)
        name=$(grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' "$plugin_dir/config.json" | sed 's/.*"\([^"]*\)"$/\1/' | head -1)
    fi

    # Start with header including version
    echo "/* $name v$version - Generated from src/ - DO NOT EDIT DIRECTLY */" > "$output"
    echo "/* Run: make plugins */" >> "$output"
    echo "" >> "$output"

    # Concatenate all _*.js files in sorted order
    for file in $(ls "$src_dir"/_*.js 2>/dev/null | sort); do
        local basename=$(basename "$file")
        echo "// === $basename ===" >> "$output"
        cat "$file" >> "$output"
        echo "" >> "$output"
    done

    # Process style.css if it exists - add version header
    if [ -f "$plugin_dir/style.css" ]; then
        local css_content=$(cat "$plugin_dir/style.css")
        # Only add header if it doesn't already have one
        if ! echo "$css_content" | head -1 | grep -q "^/\*.*v[0-9]"; then
            local temp_css=$(mktemp)
            echo "/* $name v$version */" > "$temp_css"
            # Skip any existing header comment if present
            echo "$css_content" >> "$temp_css"
            mv "$temp_css" "$plugin_dir/style.css"
        else
            # Update existing version header (use temp file for macOS compatibility)
            local temp_css=$(mktemp)
            sed "1s|/\*.*\*/|/* $name v$version */|" "$plugin_dir/style.css" > "$temp_css"
            mv "$temp_css" "$plugin_dir/style.css"
        fi
    fi

    echo "  -> $output (v$version)"
}

# If specific plugin dir provided, build just that one
if [ -n "$1" ]; then
    build_plugin "$1"
    exit 0
fi

# Otherwise, find all plugins with src/ directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

find "$ROOT_DIR/plugins" -type d -name "src" | while read src_dir; do
    plugin_dir=$(dirname "$src_dir")
    build_plugin "$plugin_dir"
done

echo "Done."

#!/bin/sh
# Renders the app icons from docs/logo/*.svg into the iPhone and Mac asset catalogs.
# Run it after changing the SVGs: ios/scripts/icons.sh (needs: brew install librsvg).
set -eu
cd "$(dirname "$0")/../.."
command -v rsvg-convert >/dev/null || { echo "rsvg-convert is missing: brew install librsvg" >&2; exit 1; }

png() { rsvg-convert -w "$2" -h "$2" "docs/logo/$1" -o "$3"; }

# iPhone: one full-bleed 1024 square; iOS rounds the corners itself.
ios=ios/App/Assets.xcassets
mkdir -p "$ios/AppIcon.appiconset"
printf '{"info":{"author":"xcode","version":1}}\n' > "$ios/Contents.json"
png icon-ios.svg 1024 "$ios/AppIcon.appiconset/icon-1024.png"
cat > "$ios/AppIcon.appiconset/Contents.json" <<'J'
{
  "images" : [
    { "filename" : "icon-1024.png", "idiom" : "universal", "platform" : "ios", "size" : "1024x1024" }
  ],
  "info" : { "author" : "xcode", "version" : 1 }
}
J

# Mac: the macOS shape (rounded tile with margin and shadow) at every size;
# 32 pixels and under use the simpler drawing so it stays legible.
mac=ios/MacApp/Assets.xcassets
mkdir -p "$mac/AppIcon.appiconset"
printf '{"info":{"author":"xcode","version":1}}\n' > "$mac/Contents.json"
images=""
for pt in 16 32 128 256 512; do
  for scale in 1 2; do
    px=$((pt * scale))
    src=icon-mac.svg
    [ "$px" -le 32 ] && src=icon-mac-small.svg
    name="icon-${pt}@${scale}x.png"
    png "$src" "$px" "$mac/AppIcon.appiconset/$name"
    images="$images${images:+,
}    { \"filename\" : \"$name\", \"idiom\" : \"mac\", \"scale\" : \"${scale}x\", \"size\" : \"${pt}x${pt}\" }"
  done
done
cat > "$mac/AppIcon.appiconset/Contents.json" <<J
{
  "images" : [
$images
  ],
  "info" : { "author" : "xcode", "version" : 1 }
}
J
echo "icons written to $ios and $mac"

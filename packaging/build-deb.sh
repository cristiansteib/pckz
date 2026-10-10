#!/bin/sh
# Build the .deb from a compiled binary.
#   packaging/build-deb.sh <version> <arch> <binary> <out-dir>
set -eu
VERSION=$1 ARCH=$2 BIN=$3 OUT=$4
ROOT=$(cd "$(dirname "$0")/.." && pwd)
APP_ID=pckz.AudioSwitcher
STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT

install -d "$STAGE/DEBIAN"
sed -e "s/@VERSION@/$VERSION/" -e "s/@ARCH@/$ARCH/" "$ROOT/packaging/debian/control" > "$STAGE/DEBIAN/control"
for f in preinst postinst prerm postrm; do
    install -m 755 "$ROOT/packaging/debian/$f" "$STAGE/DEBIAN/$f"
done

install -D -m 755 "$BIN" "$STAGE/usr/bin/audio-switcher"
install -D -m 644 "$ROOT/assets/desktop/$APP_ID.desktop" "$STAGE/usr/share/applications/$APP_ID.desktop"
install -D -m 644 "$ROOT/assets/desktop/pckz-audio-switcher-tray.desktop" "$STAGE/etc/xdg/autostart/pckz-audio-switcher-tray.desktop"
install -d "$STAGE/usr/share/icons/hicolor/scalable/apps"
install -m 644 "$ROOT"/assets/icons/*.svg "$STAGE/usr/share/icons/hicolor/scalable/apps/"
install -D -m 644 "$ROOT/LICENSE" "$STAGE/usr/share/doc/pckz-audio-switcher/copyright"

mkdir -p "$OUT"
dpkg-deb --root-owner-group -Zxz --build "$STAGE" "$OUT/pckz-audio-switcher_${VERSION}_${ARCH}.deb" >/dev/null
echo "$OUT/pckz-audio-switcher_${VERSION}_${ARCH}.deb"

#!/bin/bash
# Builds dsync for Linux and Windows and packages it:
#   dist/release/dsync-setup-VERSION-windows-amd64.exe   Windows installer
#   dist/release/dsync-VERSION-linux-x86_64.tar.gz       any Linux (install.sh inside)
#   dist/release/dsync-VERSION-1-x86_64.pkg.tar.zst      Arch Linux package
# The version comes from wails.json (info.productVersion).
#
# Needs Go, Node/npm, the Wails CLI, WebKitGTK 4.1, makepkg (for the Arch
# package) and Docker (for makensis, see packaging/nsis).
#   --skip-tests   don't run the tests first
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' wails.json)
[ -n "$version" ] || { echo "no productVersion in wails.json" >&2; exit 1; }
ldflags="-s -w -X dsync/internal/version.Version=$version"
out=dist/release
step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

for tool in go npm wails; do
	command -v "$tool" >/dev/null || { echo "missing: $tool" >&2; exit 1; }
done

if [ "${1:-}" != --skip-tests ]; then
	step "Testing"
	go test -tags webkit2_41 ./...
fi

rm -rf dist/linux dist/windows dist/arch "$out"
mkdir -p dist/linux dist/windows dist/arch "$out"

step "Building for Linux"
wails build -clean -trimpath -ldflags "$ldflags" >/dev/null
cp build/bin/dsync-gui dist/linux/
CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o dist/linux/dsync ./cmd/dsync

step "Building for Windows"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o dist/windows/dsync.exe ./cmd/dsync
PATH="$PWD/packaging/nsis:$PATH" wails build -platform windows/amd64 -nsis -trimpath -ldflags "$ldflags" >/dev/null
cp build/bin/dsync-gui.exe dist/windows/
cp "build/bin/dsync-setup-$version-windows-amd64.exe" "$out/"

step "Packaging for Linux"
name="dsync-$version-linux-x86_64"
stage="dist/linux/$name"
mkdir -p "$stage"
cp dist/linux/dsync-gui dist/linux/dsync "$stage/"
cp build/appicon.png "$stage/dsync.png"
cp packaging/linux/dsync.desktop packaging/linux/install.sh packaging/linux/uninstall.sh \
   packaging/linux/ufw-dsync packaging/linux/firewalld-dsync.xml "$stage/"
cat > "$stage/README.txt" <<README
dsync $version for Linux

Install for your user (no root needed):  ./install.sh
Remove it again:                          ./uninstall.sh

Needs WebKitGTK 4.1 (webkit2gtk-4.1 on Arch, libwebkit2gtk-4.1-0 on Debian/Ubuntu).
Other computers reach dsync on UDP 47100 and TCP 47101; install.sh offers to
open them in ufw or firewalld.
README
tar -C dist/linux -czf "$out/$name.tar.gz" "$name"

step "Packaging for Arch Linux"
cp "$out/$name.tar.gz" packaging/arch/dsync.install dist/arch/
sum=$(sha256sum "$out/$name.tar.gz" | cut -d' ' -f1)
sed -e "s/@VERSION@/$version/" -e "s/@SHA256@/$sum/" packaging/arch/PKGBUILD > dist/arch/PKGBUILD
(cd dist/arch && PKGDEST="$PWD" makepkg -f --clean >/dev/null)
cp dist/arch/dsync-"$version"-1-x86_64.pkg.tar.zst "$out/"

(cd "$out" && sha256sum -- * > SHA256SUMS)
step "Done: $out"
ls -lh "$out"

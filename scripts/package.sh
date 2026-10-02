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
#
# Windows code signing (removes "Unknown publisher"): set DSYNC_SIGN_CMD to a
# command that signs the file given as its last argument in place, e.g.
#   DSYNC_SIGN_CMD="packaging/sign/osslsigncode-pfx cert.pfx"   (a .pfx file)
#   DSYNC_SIGN_CMD="jsign --storetype TRUSTEDSIGNING ..."        (a cloud HSM)
# See packaging/sign/README.md. Without it the Windows files are unsigned.
#
# Update authentication (required for release packages):
#   DSYNC_UPDATE_PUBLIC_KEY       base64 PKIX DER Ed25519 public key
#   DSYNC_UPDATE_PRIVATE_KEY_FILE path to the matching PEM private key
# The private key is used only to sign SHA256SUMS and is never embedded.
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(sed -n 's/.*"productVersion": *"\([^"]*\)".*/\1/p' wails.json)
[ -n "$version" ] || { echo "no productVersion in wails.json" >&2; exit 1; }
: "${DSYNC_UPDATE_PUBLIC_KEY:?set DSYNC_UPDATE_PUBLIC_KEY to the base64 PKIX DER Ed25519 release public key}"
: "${DSYNC_UPDATE_PRIVATE_KEY_FILE:?set DSYNC_UPDATE_PRIVATE_KEY_FILE to the Ed25519 release private key PEM path}"
[ -f "$DSYNC_UPDATE_PRIVATE_KEY_FILE" ] || { echo "release private key not found: $DSYNC_UPDATE_PRIVATE_KEY_FILE" >&2; exit 1; }
ldflags="-s -w -X dsync/internal/version.Version=$version -X dsync/internal/update.releasePublicKeyDERBase64=$DSYNC_UPDATE_PUBLIC_KEY"
out=dist/release
step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

for tool in go npm wails openssl base64; do
	command -v "$tool" >/dev/null || { echo "missing: $tool" >&2; exit 1; }
done

actual_public_key=$(openssl pkey -in "$DSYNC_UPDATE_PRIVATE_KEY_FILE" -pubout -outform DER | base64 | tr -d '\n')
[ "$actual_public_key" = "$DSYNC_UPDATE_PUBLIC_KEY" ] || {
	echo "DSYNC_UPDATE_PUBLIC_KEY does not match DSYNC_UPDATE_PRIVATE_KEY_FILE" >&2
	exit 1
}

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

# sign FILE signs a Windows program in place when DSYNC_SIGN_CMD is set.
sign() {
	[ -n "${DSYNC_SIGN_CMD:-}" ] || return 0
	echo "    signing $(basename "$1")"
	$DSYNC_SIGN_CMD "$1"
}

step "Building for Windows"
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o dist/windows/dsync.exe ./cmd/dsync
sign dist/windows/dsync.exe
PATH="$PWD/packaging/nsis:$PATH" wails build -platform windows/amd64 -nsis -trimpath -ldflags "$ldflags" >/dev/null
setup="build/bin/dsync-setup-$version-windows-amd64.exe"
if [ -n "${DSYNC_SIGN_CMD:-}" ]; then
	# Wails packs the program into the installer as it builds it, so sign the
	# program, pack it again, then sign the installer.
	sign build/bin/dsync-gui.exe
	(cd build/windows/installer && "$OLDPWD/packaging/nsis/makensis" -V2 \
		"-DARG_WAILS_AMD64_BINARY=$OLDPWD/build/bin/dsync-gui.exe" project.nsi)
	sign "$setup"
fi
cp build/bin/dsync-gui.exe dist/windows/
cp "$setup" "$out/"

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
openssl pkeyutl -sign -rawin -inkey "$DSYNC_UPDATE_PRIVATE_KEY_FILE" \
	-in "$out/SHA256SUMS" -out "$out/SHA256SUMS.sig"
step "Done: $out"
ls -lh "$out"

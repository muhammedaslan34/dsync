package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// How this copy of dsync was installed decides how it updates itself.
const (
	KindInstaller = "installer" // the Windows setup
	KindPacman    = "pacman"    // the Arch package
	KindLocal     = "local"     // install.sh from the Linux tarball
	KindMacApp    = "macapp"    // dsync.app from the macOS disk image
	KindManual    = "manual"    // anything else, e.g. a development build
)

// AssetName is the release file an install of this kind updates from.
func AssetName(kind, version string) string {
	switch kind {
	case KindInstaller:
		return fmt.Sprintf("dsync-setup-%s-windows-amd64.exe", version)
	case KindPacman:
		return fmt.Sprintf("dsync-%s-1-x86_64.pkg.tar.zst", version)
	case KindLocal:
		return fmt.Sprintf("dsync-%s-linux-x86_64.tar.gz", version)
	case KindMacApp:
		return fmt.Sprintf("dsync-%s-macos-universal.dmg", version)
	}
	return ""
}

// installLocal replaces the programs in dir (normally ~/.local/lib/dsync)
// with the ones in the release tarball. Each file is written next to the
// old one and renamed over it, so a running dsync keeps working until it
// restarts.
func installLocal(tarball, dir string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	want := map[string]bool{"dsync-gui": true, "dsync": true}
	got := map[string]bool{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		// Only "dsync-VERSION-linux-x86_64/<program>", never "../<program>".
		parts := strings.Split(strings.Trim(h.Name, "/"), "/")
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "dsync-") || !want[parts[1]] {
			continue
		}
		name := parts[1]
		tmp := filepath.Join(dir, "."+name+".new")
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, io.LimitReader(tr, 512<<20))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp, filepath.Join(dir, name))
		}
		if err != nil {
			os.Remove(tmp)
			return err
		}
		got[name] = true
	}
	if len(got) != len(want) {
		return errors.New("the update is missing files; nothing was changed")
	}
	return nil
}

package wlipc

import (
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// legacyConfigDirs maps the FyneDesk-era configuration directories (relative
// to the user config dir) to their Tyde names.
var legacyConfigDirs = [][2]string{
	{"fynedesk", "tyde"},
	{filepath.Join("fyne", "com.fyshos.fynedesk"), filepath.Join("fyne", "com.fyshos.tyde")},
}

// MigrateLegacyConfig copies the configuration written by FyneDesk (before
// the project became Tyde) to the Tyde locations, once: a destination that
// already exists is left alone. The old directories are kept so that an
// older build still finds its settings.
func MigrateLegacyConfig() {
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	migrateLegacyConfig(base)
}

func migrateLegacyConfig(base string) {
	for _, d := range legacyConfigDirs {
		src, dst := filepath.Join(base, d[0]), filepath.Join(base, d[1])
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		// Copy into a temporary sibling then rename, so an interrupted copy
		// is retried next time instead of leaving a partial configuration.
		tmp := dst + ".migrating"
		_ = os.RemoveAll(tmp)
		if err := copyTree(src, tmp); err != nil {
			log.Printf("[MIGRATE] copying %s failed: %v", src, err)
			_ = os.RemoveAll(tmp)
			continue
		}
		if err := os.Rename(tmp, dst); err != nil {
			log.Printf("[MIGRATE] installing %s failed: %v", dst, err)
			_ = os.RemoveAll(tmp)
			continue
		}
		log.Printf("[MIGRATE] copied %s to %s", src, dst)
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil // sockets, fifos: runtime leftovers, not configuration
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

//go:build linux

package fs

import "golang.org/x/sys/unix"

func exclusivePublish(dirfd int, tempName, destName, path string) error {
	// Prefer RENAME_NOREPLACE: atomic publish that never overwrites an
	// existing destination. Some filesystems (notably WSL's v9fs over a
	// Windows mount) reject the flag with EINVAL even though their base
	// rename works, so fall back to the link+unlink primitive that the
	// non-Linux Unix variants use. Both constructions give the same
	// "existing destination is an error" guarantee.
	err := unix.Renameat2(dirfd, tempName, dirfd, destName, unix.RENAME_NOREPLACE)
	if err == unix.EEXIST {
		return existError("write", path, "protected file already exists")
	}
	if err == nil {
		return nil
	}
	if err != unix.EINVAL {
		return mapOpenErr("rename", path, err)
	}
	return publishViaLink(dirfd, tempName, destName, path)
}

// publishViaLink atomically publishes tempName as destName without
// overwriting an existing destination, using link+unlink. It is used as a
// fallback when RENAME_NOREPLACE is rejected with EINVAL (e.g. WSL's v9fs
// over a Windows mount), and is kept as a small pure function so the
// fallback path can be tested in isolation.
func publishViaLink(dirfd int, tempName, destName, path string) error {
	if err := unix.Linkat(dirfd, tempName, dirfd, destName, 0); err != nil {
		if err == unix.EEXIST {
			return existError("write", path, "protected file already exists")
		}
		return mapOpenErr("link", path, err)
	}
	_ = unix.Unlinkat(dirfd, tempName, 0)
	return nil
}

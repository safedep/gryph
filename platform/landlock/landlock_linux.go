package landlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unsafe"

	"github.com/safedep/dry/log"
	"golang.org/x/sys/unix"
)

// The access rights of each ABI version. A ruleset handles only the rights
// the kernel knows, else the create call fails.
const (
	accessRead = unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR | unix.LANDLOCK_ACCESS_FS_EXECUTE
	accessV1   = accessRead | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM
	accessV2 = accessV1 | unix.LANDLOCK_ACCESS_FS_REFER
	accessV3 = accessV2 | unix.LANDLOCK_ACCESS_FS_TRUNCATE
	accessV5 = accessV3 | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	// accessFile are the rights that apply to a file. A rule on a file
	// with a directory right is refused by the kernel.
	accessFile = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
)

func abi() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("landlock: %w: %v", ErrUnsupported, errno)
	}
	return int(v), nil
}

// handledAccess returns the rights the kernel of this ABI knows.
func handledAccess(version int) uint64 {
	switch {
	case version >= 5:
		return accessV5
	case version >= 3:
		return accessV3
	case version >= 2:
		return accessV2
	}
	return accessV1
}

func restrict(opts Options) error {
	version, err := abi()
	if err != nil {
		return err
	}
	handled := handledAccess(version)
	rules, err := plan(opts)
	if err != nil {
		return err
	}
	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock: create the ruleset: %v", errno)
	}
	defer func() { _ = unix.Close(int(fd)) }()
	for _, r := range rules {
		if err := addRule(int(fd), r.path, r.access&handled); err != nil {
			return err
		}
	}
	if _, _, errno := unix.RawSyscall(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); errno != 0 {
		return fmt.Errorf("landlock: set no_new_privs: %v", errno)
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: restrict self: %v", errno)
	}
	return nil
}

// rule is one path with the rights the ruleset grants beneath it.
type rule struct {
	path   string
	access uint64
}

// plan builds the rules: the read rights on each read-only path, and every
// right on each entry of each ancestor that is not on the way to a
// read-only path. A path on the way to one read-only path and a sibling
// of another keeps the narrower rights.
func plan(opts Options) ([]rule, error) {
	if len(opts.ReadOnly) == 0 {
		return nil, errors.New("landlock: nothing to protect")
	}
	onPath := map[string]bool{}
	var protected []string
	for _, p := range opts.ReadOnly {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		abs, err = filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("landlock: %s: %w", p, err)
		}
		protected = append(protected, abs)
		for dir := abs; ; dir = filepath.Dir(dir) {
			onPath[dir] = true
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	access := map[string]uint64{}
	for _, p := range protected {
		access[p] = accessRead
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			access[p] = accessRead & accessFile
		}
		for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return nil, fmt.Errorf("landlock: list %s: %w", dir, err)
			}
			for _, e := range entries {
				path := filepath.Join(dir, e.Name())
				if onPath[path] || e.Type()&os.ModeSymlink != 0 {
					continue
				}
				if _, seen := access[path]; !seen {
					access[path] = accessV5
					if !e.IsDir() {
						access[path] = accessFile
					}
				}
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	paths := make([]string, 0, len(access))
	for p := range access {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	rules := make([]rule, 0, len(paths))
	for _, p := range paths {
		rules = append(rules, rule{path: p, access: access[p]})
	}
	return rules, nil
}

func addRule(ruleset int, path string, access uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		// An entry the account cannot open gets no rule and stays out of
		// reach, as it was.
		log.Debugf("landlock: no rule for %s: %v", path, err)
		return nil
	}
	defer func() { _ = unix.Close(fd) }()
	attr := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(fd)}
	if _, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, uintptr(ruleset), unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0); errno != 0 {
		return fmt.Errorf("landlock: rule for %s: %v", path, errno)
	}
	return nil
}

func ancestors(opts Options) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range opts.ReadOnly {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

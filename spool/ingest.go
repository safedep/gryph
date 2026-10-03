package spool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/decision/ipc"
	"github.com/safedep/gryph/platform/nofollow"
)

// Limits bound what one account's spool can ask of the service in one
// pass. A same-user process can fill its own spool; the limits keep that
// from filling the store or holding the pass.
type Limits struct {
	// MaxFileBytes is the size above which a file is refused unread.
	MaxFileBytes int64
	// MaxFiles is the count of entries one pass takes from one account.
	// The rest are removed unread and counted as dropped.
	MaxFiles int
	// MaxBytes is the sum of the sizes one pass reads from one account.
	MaxBytes int64
}

// DefaultLimits are the limits of a service without configuration. One
// entry holds one frame, so the file cap is the frame cap plus the room
// the entry's own fields take.
func DefaultLimits() Limits {
	return Limits{MaxFileBytes: ipc.MaxFrame + 64*1024, MaxFiles: 256, MaxBytes: 64 * 1024 * 1024}
}

// Refused names one file the pass did not take and why.
type Refused struct {
	Name   string
	Reason string
}

// Report is what one pass found in the spool of one account next to the
// entries it took.
type Report struct {
	// Refused are the files the pass removed unread: another owner, not
	// a regular file, more than one link, too large, or not an entry.
	Refused []Refused
	// Dropped is the count of entries removed unread because the account
	// was over its quota for the pass.
	Dropped int
}

// Sink receives what one pass takes from the spool, per account.
type Sink interface {
	// Entry records one entry of the account uid. An error keeps the
	// file for the next pass and ends the pass for the account.
	Entry(ctx context.Context, uid uint32, entry Entry) error
	// Report receives what the pass refused or dropped for the account,
	// when there is anything.
	Report(ctx context.Context, uid uint32, report Report)
}

// Ingest runs one pass over the spool at root. The owner of an account
// directory decides the account: a directory named for another uid still
// feeds the partition of its owner, and a file with another owner inside
// it is refused. Every file is opened relative to the directory handle,
// without following a link, and its kind, owner and link count are read
// from the open descriptor. A file the pass took, refused or dropped is
// removed.
func Ingest(ctx context.Context, root *nofollow.Dir, limits Limits, sink Sink) error {
	entries, err := root.ReadDir()
	if err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ingestDir(ctx, root, e.Name(), limits, sink); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func ingestDir(ctx context.Context, root *nofollow.Dir, name string, limits Limits, sink Sink) error {
	dir, err := root.OpenDir(name)
	if err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	defer func() { _ = dir.Close() }()
	info, err := dir.Info()
	if err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	owner, _, ok := nofollow.Owner(info)
	if !ok {
		return fmt.Errorf("spool: %s: no owner information", dir.Path())
	}
	if named, err := strconv.ParseUint(name, 10, 32); err != nil || uint32(named) != owner {
		log.Warnf("spool: directory %s is owned by uid %d: its entries go to that account", dir.Path(), owner)
	}

	files, err := dir.ReadDir()
	if err != nil {
		return fmt.Errorf("spool: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	var report Report
	var taken int
	var bytes int64
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") {
			continue
		}
		if taken >= limits.MaxFiles || bytes >= limits.MaxBytes {
			report.Dropped++
			remove(dir, f.Name())
			continue
		}
		entry, size, reason := readEntry(dir, f.Name(), owner, limits.MaxFileBytes)
		if reason != "" {
			report.Refused = append(report.Refused, Refused{Name: f.Name(), Reason: reason})
			remove(dir, f.Name())
			continue
		}
		if err := sink.Entry(ctx, owner, entry); err != nil {
			return fmt.Errorf("spool: %s: %w", f.Name(), err)
		}
		taken++
		bytes += size
		remove(dir, f.Name())
	}
	if len(report.Refused) > 0 || report.Dropped > 0 {
		sink.Report(ctx, owner, report)
	}
	return nil
}

// readEntry reads one spool file. The kind, the owner and the link count
// come from the open descriptor, so a swap of the name after the checks
// changes nothing. A reason names a refused file.
func readEntry(dir *nofollow.Dir, name string, owner uint32, maxBytes int64) (Entry, int64, string) {
	f, err := dir.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Entry{}, 0, ""
		}
		return Entry{}, 0, err.Error()
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return Entry{}, 0, err.Error()
	}
	uid, links, ok := nofollow.Owner(info)
	switch {
	case !ok:
		return Entry{}, 0, "no owner information"
	case uid != owner:
		return Entry{}, 0, fmt.Sprintf("owned by uid %d, the directory by uid %d", uid, owner)
	case links != 1:
		return Entry{}, 0, fmt.Sprintf("%d links", links)
	case info.Size() > maxBytes:
		return Entry{}, 0, fmt.Sprintf("%d bytes, over the %d byte cap", info.Size(), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return Entry{}, 0, err.Error()
	}
	if int64(len(data)) > maxBytes {
		return Entry{}, 0, fmt.Sprintf("grew over the %d byte cap", maxBytes)
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, 0, "not an entry: " + err.Error()
	}
	if entry.Frame != nil {
		if _, err := ipc.Decode(entry.Frame); err != nil {
			return Entry{}, 0, "frame refused: " + err.Error()
		}
	}
	return entry, int64(len(data)), ""
}

func remove(dir *nofollow.Dir, name string) {
	if err := dir.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warnf("spool: %s not removed: %v", name, err)
	}
}

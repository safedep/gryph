package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/safedep/dry/log"
	"github.com/safedep/gryph/config"
	"github.com/safedep/gryph/engine"
	"github.com/safedep/gryph/platform/nofollow"
)

// collectionLevelFile holds the collection level the account was last
// told about, in the partition. A level that differs from it is a change
// the developer has not seen.
const collectionLevelFile = "collection.level"

// collectionNoticeFormat is the one line a hook carries after the level
// of the host was set or changed. The log of the account stays readable
// at every level, and the line says so.
const collectionNoticeFormat = "Collection level %s is in force on this host. Your log stays readable with gryph logs."

// collectionNotice returns the notice of a collection level the account
// has not seen yet, and records the change. The caller holds the write
// lock. The level comes from the managed configuration the service
// started with, so a change reaches the account on the next start. A
// managed file that sets no level gets no notice: nothing leaves the
// host until a target exists, and the notice is for a level the
// administrator stated.
func (p *partition) collectionNotice(ctx context.Context) []string {
	if p.rt.Config.Collection.Level == "" {
		return nil
	}
	level := p.rt.Config.Collection.EffectiveLevel()
	if level == config.CollectionNone {
		return nil
	}
	path := filepath.Join(p.dir, collectionLevelFile)
	seen, err := nofollow.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warnf("supervisor: collection level of uid %d: %v", p.uid, err)
		return nil
	}
	previous := string(bytes.TrimSpace(seen))
	if previous == level {
		return nil
	}
	if err := nofollow.WriteFile(path, []byte(level+"\n"), 0o600); err != nil {
		log.Warnf("supervisor: collection level of uid %d not recorded: %v", p.uid, err)
		return nil
	}
	details := map[string]interface{}{"level": level, "previous": previous, "profile": p.rt.Config.Collection.Profile()}
	if err := engine.LogSelfAudit(ctx, p.rt.Store, engine.SelfAuditActionCollectionLevel, "", details, engine.SelfAuditResultSuccess, ""); err != nil {
		log.Warnf("supervisor: collection level audit of uid %d: %v", p.uid, err)
	}
	return []string{fmt.Sprintf(collectionNoticeFormat, level)}
}

package cache

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// TagSet is the set of tags a TaggedCache writes under.
//
// It answers Illuminate\Cache\TagSet. Each tag is one ordinary cache entry
// holding a generation id, and the set's namespace is those ids joined. Every
// key a tagged repository builds carries a digest of that namespace, so
// changing one id orphans every entry carrying that tag at once -- one write,
// however many millions of entries. Nothing is scanned and nothing is deleted;
// the orphans expire on their own ttl.
//
// The generations are tenant-scoped like everything else (RULE 14): two tenants
// tagging with "invoices" have separate generations, so one flushing its tag
// cannot orphan the other's entries.
type TagSet struct {
	store Store
	names []string
}

// NewTagSet returns the set. It answers TagSet::__construct().
//
// It does not touch the store: the generations are read, and minted if they are
// not there, the first time a key has to be built.
func NewTagSet(s Store, names []string) *TagSet {
	out := make([]string, len(names))
	copy(out, names)
	return &TagSet{store: s, names: out}
}

// GetNames returns the tag names in the set. It answers TagSet::getNames().
func (t *TagSet) GetNames() []string {
	out := make([]string, len(t.names))
	copy(out, t.names)
	return out
}

// TagKey is the cache key one tag's generation is stored under.
//
// It answers TagSet::tagKey(), which is 'tag:'.$name.':key'. The tenant is in
// it here, because a generation shared between tenants would let one of them
// flush the other's entries.
func (t *TagSet) TagKey(g auth.Grant, name string) (string, error) {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return "", ErrNoTenant
	}
	if !auth.ValidTenant(tenant) {
		return "", fmt.Errorf("%w: %q cannot be part of a key", ErrNoTenant, tenant)
	}
	if name == "" {
		return "", errors.New("cache: an empty tag is not a tag")
	}
	if strings.Contains(name, ":") {
		return "", fmt.Errorf("cache: the tag %q contains a colon, which is the key separator, so it can name another tag's generation", name)
	}
	return "cache:" + tenant + ":tag:" + name + ":key", nil
}

// TagID returns a tag's current generation, minting one if there is none.
//
// It answers TagSet::tagId(). A tag nobody has used yet is not an error: the
// first read creates the generation, exactly as the PHP does, so tagging works
// on a cold cache.
func (t *TagSet) TagID(ctx context.Context, g auth.Grant, name string) (string, error) {
	key, err := t.TagKey(g, name)
	if err != nil {
		return "", err
	}
	switch raw, err := t.store.Get(ctx, key); {
	case err == nil && len(raw) > 0:
		return string(raw), nil
	case err == nil, errors.Is(err, ErrNotFound):
		return t.ResetTag(ctx, g, name)
	default:
		return "", err
	}
}

// ResetTag gives a tag a new generation and returns it.
//
// It answers TagSet::resetTag(). Everything cached under the old generation is
// now unreachable, which is what flushing a tag is.
//
// The generation is time plus randomness, like the uniqid() Laravel uses, and
// for the same reason: two processes resetting the same tag in the same
// instant must not agree on the new id, or one of them keeps serving what the
// other flushed.
func (t *TagSet) ResetTag(ctx context.Context, g auth.Grant, name string) (string, error) {
	key, err := t.TagKey(g, name)
	if err != nil {
		return "", err
	}
	id, err := newTagID()
	if err != nil {
		return "", err
	}
	if err := t.store.Put(ctx, key, []byte(id), foreverTTL); err != nil {
		return "", err
	}
	return id, nil
}

// Reset gives every tag in the set a new generation.
//
// It answers TagSet::reset(). It is what TaggedCache.Flush is built on.
func (t *TagSet) Reset(ctx context.Context, g auth.Grant) error {
	for _, name := range t.names {
		if _, err := t.ResetTag(ctx, g, name); err != nil {
			return err
		}
	}
	return nil
}

// FlushTag removes a tag's generation entirely.
//
// It answers TagSet::flushTag(). The difference from ResetTag is what is left
// behind: Reset writes a new generation, Flush writes none, and the next read
// mints one. Both orphan the same entries.
func (t *TagSet) FlushTag(ctx context.Context, g auth.Grant, name string) error {
	key, err := t.TagKey(g, name)
	if err != nil {
		return err
	}
	return t.store.Forget(ctx, key)
}

// Flush removes every tag's generation. It answers TagSet::flush().
func (t *TagSet) Flush(ctx context.Context, g auth.Grant) error {
	for _, name := range t.names {
		if err := t.FlushTag(ctx, g, name); err != nil {
			return err
		}
	}
	return nil
}

// GetNamespace is the set's generations joined, and it changes whenever any tag
// in the set is reset or flushed.
//
// It answers TagSet::getNamespace().
func (t *TagSet) GetNamespace(ctx context.Context, g auth.Grant) (string, error) {
	ids := make([]string, 0, len(t.names))
	for _, name := range t.names {
		id, err := t.TagID(ctx, g, name)
		if err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	return strings.Join(ids, "|"), nil
}

// taggedItemKey is the namespace digest and the caller's key, which is what
// TaggedCache::taggedItemKey builds.
func (t *TagSet) taggedItemKey(ctx context.Context, g auth.Grant, key string) (string, error) {
	namespace, err := t.GetNamespace(ctx, g)
	if err != nil {
		return "", err
	}
	return digest(namespace) + ":" + key, nil
}

// digest is the sha1 Laravel takes of the namespace, and it is here for the
// same reason: the namespace grows with the number of tags and with every
// reset, and a key of unbounded length is a key some backend will refuse.
//
// It is not a security boundary. Nothing is hidden by it and nothing is
// authenticated with it -- the tenant, which is the boundary, is in the clear
// in front of it.
func digest(namespace string) string {
	sum := sha1.Sum([]byte(namespace))
	return hex.EncodeToString(sum[:])
}

// newTagID mints a generation.
func newTagID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("cache: reading random bytes for a tag generation: %w", err)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:]), nil
}

// TaggedCache is a Repository whose every key carries a tag generation.
//
// It answers Illuminate\Cache\TaggedCache, and like the PHP it is the whole
// Repository surface again -- Put, Get, Remember, Forget and the rest are the
// methods of the embedded Repository, and they behave identically. What changes
// is where the entries land and what Flush does.
//
// The one thing to know before reaching for it: a tagged entry can only be
// reached through the same tags. Cache.Tags("a").Put and Cache.Put write two
// different entries, and so do Tags("a", "b") and Tags("b", "a") -- order is
// part of the namespace, in this and in Laravel.
type TaggedCache struct {
	*Repository
}

// Flush orphans every entry carrying these tags.
//
// It answers TaggedCache::flush(). It resets the generations rather than
// deleting entries: one write per tag, whatever the entry count, and the
// orphaned entries go when their own ttl does.
//
// That is the trade the tag mechanism is: flushing is O(tags) instead of
// O(entries), and the price is that flushed entries occupy the store until they
// expire. Forever entries under a flushed tag occupy it for a century, which is
// the strongest argument this package has against Forever.
func (t *TaggedCache) Flush(ctx context.Context, g auth.Grant) error {
	return t.tags.Reset(ctx, g)
}

// Clear is Flush, as it is on Repository. It answers the clear() TaggedCache
// inherits, resolved against the tagged flush().
func (t *TaggedCache) Clear(ctx context.Context, g auth.Grant) error {
	return t.Flush(ctx, g)
}

// TaggedItemKey is the key an item is really stored under.
//
// It answers TaggedCache::taggedItemKey(). It is exported for the same reason
// it is public in Laravel: something eventually has to look in the store and
// find out where the entry went.
func (t *TaggedCache) TaggedItemKey(ctx context.Context, g auth.Grant, key string) (string, error) {
	return t.key(ctx, g, key)
}

// GetTags returns the tag set. It answers TaggedCache::getTags().
func (t *TaggedCache) GetTags() *TagSet { return t.tags }

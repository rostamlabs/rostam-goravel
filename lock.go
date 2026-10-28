package rostam

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	contractscache "github.com/goravel/framework/contracts/cache"
)

var _ contractscache.Lock = &Lock{}

// Lock is a lock with an owner token.
//
// The framework's own lock releases with a plain delete, so a process whose
// lock already expired deletes the next holder's lock. This one stores a
// random token and releases with compare-and-delete: Release frees the lock
// only while the token still matches.
type Lock struct {
	cache *Cache
	name  string
	owner string
	ttl   time.Duration
}

// NewLock creates a lock for name. Without a duration, or with a duration
// of zero or less, the lock does not expire on its own.
func NewLock(cache *Cache, name string, t ...time.Duration) *Lock {
	lock := &Lock{cache: cache, name: name, owner: newOwner()}
	if len(t) > 0 && t[0] > 0 {
		lock.ttl = t[0]
	}

	return lock
}

func (r *Lock) Block(t time.Duration, callback ...func()) bool {
	return r.BlockWithTicker(t, 1*time.Second, callback...)
}

// BlockWithTicker tries to acquire the lock every ticker until t has passed,
// with a last attempt at the deadline. It gives up early when the store's
// context is cancelled.
func (r *Lock) BlockWithTicker(t time.Duration, ticker time.Duration, callback ...func()) bool {
	if r.Get(callback...) {
		return true
	}

	deadline := time.NewTimer(t)
	defer deadline.Stop()
	tick := time.NewTicker(ticker)
	defer tick.Stop()

	for {
		select {
		case <-r.cache.ctx.Done():
			return false
		case <-deadline.C:
			return r.Get(callback...)
		case <-tick.C:
			if r.Get(callback...) {
				return true
			}
		}
	}
}

// Get acquires the lock. With a callback, it runs the callback while holding
// the lock and returns whether the release afterwards succeeded.
func (r *Lock) Get(callback ...func()) bool {
	acquired, err := r.cache.instance.SetNX(r.cache.ctx, r.key(), []byte(r.owner), wireTTL(r.ttl))
	if err != nil || !acquired {
		return false
	}

	if len(callback) == 0 {
		return true
	}

	callback[0]()

	return r.Release()
}

// Release frees the lock if this owner still holds it.
func (r *Lock) Release() bool {
	released, err := r.cache.instance.CompareAndDelete(r.cache.ctx, r.key(), []byte(r.owner))

	return err == nil && released
}

// ForceRelease frees the lock whoever holds it.
func (r *Lock) ForceRelease() bool {
	return kvDel(r.cache.ctx, r.cache.instance, r.cache.lockKey(r.name)) == nil
}

// Owner returns the token, for RestoreLock in another process.
func (r *Lock) Owner() string {
	return r.owner
}

// Extend resets the lock's expiry to t from now, if this owner still holds
// it. A duration of zero or less returns false rather than making the lock
// permanent.
func (r *Lock) Extend(t time.Duration) bool {
	if t <= 0 {
		return false
	}

	extended, err := r.cache.instance.CompareAndExpire(r.cache.ctx, r.key(), []byte(r.owner), wireTTL(t))
	if err != nil || !extended {
		return false
	}

	r.ttl = t

	return true
}

func (r *Lock) key() []byte {
	return []byte(r.cache.lockKey(r.name))
}

func newOwner() string {
	token := make([]byte, 16)
	_, _ = rand.Read(token) // crypto/rand.Read never fails since Go 1.24

	return hex.EncodeToString(token)
}

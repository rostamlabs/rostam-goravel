package rostam

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	contractscache "github.com/goravel/framework/contracts/cache"
	"github.com/goravel/framework/contracts/config"
	contractshttp "github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/process"
	contractsdocker "github.com/goravel/framework/contracts/testing/docker"
	frameworkerrors "github.com/goravel/framework/errors"
	"github.com/rostamlabs/rostam/client"
	"github.com/spf13/cast"
)

var _ contractscache.Driver = &Cache{}

// counterConversionAttempts bounds how often Increment retries turning a
// numeric string into a counter while other writers keep changing it.
const counterConversionAttempts = 3

type Cache struct {
	ctx        context.Context
	config     config.Config
	process    process.Process
	instance   *client.Client
	namespace  namespace
	prefix     string
	store      string
	connection string
}

// NewCache reads cache.stores.<store>:
//
//	connection     string         database.rostam connection, default "default"
//	prefix         string         defaults to cache.prefix
//	flush          string         epoch (default), server or unsupported
//	epoch_refresh  time.Duration  how stale the cached flush generation may be, default 10s
func NewCache(ctx context.Context, config config.Config, process process.Process, store string) (*Cache, error) {
	storePrefix := fmt.Sprintf("cache.stores.%s", store)
	connection := config.GetString(storePrefix+".connection", "default")

	instance, err := GetClient(config, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to init rostam client: %w", err)
	}

	prefix := config.GetString(storePrefix+".prefix", config.GetString("cache.prefix"))
	epochRefresh := config.GetDuration(storePrefix+".epoch_refresh", 10*time.Second)

	keys, err := newNamespace(config.GetString(storePrefix+".flush", FlushEpoch), store, prefix, instance, connection, epochRefresh)
	if err != nil {
		return nil, err
	}

	return &Cache{
		ctx:        ctx,
		config:     config,
		process:    process,
		instance:   instance,
		namespace:  keys,
		prefix:     prefix,
		store:      store,
		connection: connection,
	}, nil
}

// Add stores an item only if the key does not exist. A zero duration stores
// it without expiry; a negative one stores nothing.
func (r *Cache) Add(key string, value any, t time.Duration) bool {
	if t < 0 {
		return false
	}

	stored, err := encodeValue(value)
	if err != nil {
		return false
	}

	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return false
	}

	added, err := r.instance.SetNX(r.ctx, []byte(qualified), stored, wireTTL(t))

	return err == nil && added
}

func (r *Cache) Decrement(key string, value ...int64) (int64, error) {
	if len(value) == 0 {
		value = append(value, 1)
	}

	return r.Increment(key, -value[0])
}

func (r *Cache) Docker() (contractsdocker.CacheDriver, error) {
	if r.process == nil {
		return nil, frameworkerrors.ProcessFacadeNotSet
	}

	return NewDocker(r.config, r.process, r.store)
}

// Forever stores an item without expiry.
func (r *Cache) Forever(key string, value any) bool {
	return r.Put(key, value, 0) == nil
}

// Forget removes an item from the cache.
func (r *Cache) Forget(key string) bool {
	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return false
	}

	return kvDel(r.ctx, r.instance, qualified) == nil
}

// Flush removes every item of this store. With the default epoch mode only
// this store's prefix is retired; see the flush option for the others.
func (r *Cache) Flush() bool {
	return r.namespace.flush(r.ctx) == nil
}

// Get retrieves an item. Values come back as strings, counters as their
// decimal form, the same as goravel/redis.
func (r *Cache) Get(key string, def ...any) any {
	value, err := r.get(key)
	if err != nil {
		if len(def) == 0 {
			return nil
		}

		switch s := def[0].(type) {
		case func() any:
			return s()
		default:
			return s
		}
	}

	return value
}

func (r *Cache) GetBool(key string, def ...bool) bool {
	if len(def) == 0 {
		def = append(def, false)
	}
	// "1" and "0" are how a bool is stored; "true", "false" and the rest of
	// strconv.ParseBool's spellings are what a caller may have stored as a
	// string. Anything else reads as false.
	return cast.ToBool(r.Get(key, def[0]))
}

func (r *Cache) GetInt(key string, def ...int) int {
	if len(def) == 0 {
		def = append(def, 1)
	}
	res := r.Get(key, def[0])
	if val, ok := res.(string); ok {
		i, err := strconv.Atoi(val)
		if err != nil {
			return def[0]
		}

		return i
	}

	return cast.ToInt(res)
}

func (r *Cache) GetInt64(key string, def ...int64) int64 {
	if len(def) == 0 {
		def = append(def, 1)
	}
	res := r.Get(key, def[0])
	if val, ok := res.(string); ok {
		i, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return def[0]
		}

		return i
	}

	return cast.ToInt64(res)
}

func (r *Cache) GetString(key string, def ...string) string {
	if len(def) == 0 {
		def = append(def, "")
	}

	return cast.ToString(r.Get(key, def[0]))
}

// Has checks whether an item exists.
func (r *Cache) Has(key string) bool {
	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return false
	}

	exists, err := r.instance.Exists(r.ctx, []byte(qualified))

	return err == nil && exists
}

// Increment adds to a counter atomically, creating it at the given value when
// it is missing. The key keeps whatever expiry it already had, like Redis
// INCRBY. A key holding a numeric string, for example from Put("k", "5"), is
// converted to a counter first; any other value returns ErrNotACounter.
func (r *Cache) Increment(key string, value ...int64) (int64, error) {
	if len(value) == 0 {
		value = append(value, 1)
	}

	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return 0, err
	}

	for range counterConversionAttempts {
		total, err := r.instance.IncrEx(r.ctx, []byte(qualified), value[0], 0)

		var remote *client.RemoteError
		if !errors.As(err, &remote) {
			return total, err
		}

		// The server answers a non-counter value with a generic error, the same
		// one it gives for other failures, so look at the value to tell which.
		converted, convertErr := r.convertToCounter(key, qualified)
		if convertErr != nil {
			return 0, convertErr
		}
		if !converted {
			return 0, err
		}
	}

	return 0, ErrNotACounter.Args(key)
}

// convertToCounter rewrites a numeric string as a counter, keeping its
// remaining expiry. It reports false when the stored value already is a
// counter, so the increment failed for some other reason.
func (r *Cache) convertToCounter(key, qualified string) (bool, error) {
	stored, err := kvGet(r.ctx, r.instance, qualified)
	if errors.Is(err, client.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if len(stored) == counterSize {
		return false, nil
	}

	text, err := decodeValue(stored)
	if err != nil {
		return false, ErrNotACounter.Args(key)
	}

	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return false, ErrNotACounter.Args(key)
	}

	remaining, err := r.instance.TTL(r.ctx, []byte(qualified))
	if err != nil {
		return false, err
	}
	if remaining == -2 {
		return true, nil
	}

	// A key with 0ms left is still a key with an expiry; written with a zero
	// TTL it would never expire at all.
	var ttl time.Duration
	if remaining >= 0 {
		ttl = time.Duration(max(remaining, 1)) * time.Millisecond
	}

	// A lost race leaves the value to whoever changed it; the caller retries.
	if _, err := r.instance.CAS(r.ctx, []byte(qualified), encodeCounter(number), stored, ttl); err != nil {
		return false, err
	}

	return true, nil
}

// Lock returns an owner-token lock: Release frees it only while this
// instance still holds it.
func (r *Cache) Lock(key string, t ...time.Duration) contractscache.Lock {
	return NewLock(r, key, t...)
}

// RestoreLock rebuilds a lock acquired elsewhere from its owner token, so a
// different process can release or extend it.
func (r *Cache) RestoreLock(key, owner string, t ...time.Duration) *Lock {
	lock := NewLock(r, key, t...)
	lock.owner = owner

	return lock
}

// Put stores an item for the given duration. Zero stores it without expiry;
// a negative duration removes the key, as if it had already expired.
func (r *Cache) Put(key string, value any, t time.Duration) error {
	stored, err := encodeValue(value)
	if err != nil {
		return err
	}

	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return err
	}

	if t < 0 {
		return kvDel(r.ctx, r.instance, qualified)
	}

	return kvPut(r.ctx, r.instance, qualified, stored, wireTTL(t))
}

// Pull retrieves an item and deletes it in one atomic step.
func (r *Cache) Pull(key string, def ...any) any {
	value, err := r.pull(key)
	if err != nil {
		if len(def) == 0 {
			return nil
		}

		switch s := def[0].(type) {
		case func() any:
			return s()
		default:
			return s
		}
	}

	return value
}

// Remember gets an item, or stores the callback's result for the given duration.
func (r *Cache) Remember(key string, ttl time.Duration, callback func() (any, error)) (any, error) {
	val := r.Get(key, nil)

	if val != nil {
		return val, nil
	}

	var err error
	val, err = callback()
	if err != nil {
		return nil, err
	}

	if err := r.Put(key, val, ttl); err != nil {
		return nil, err
	}

	return val, nil
}

// RememberForever gets an item, or stores the callback's result without expiry.
func (r *Cache) RememberForever(key string, callback func() (any, error)) (any, error) {
	return r.Remember(key, 0, callback)
}

// WithContext returns a copy of the store whose calls use ctx. It shares the
// connection and the flush generation with the original.
func (r *Cache) WithContext(ctx context.Context) contractscache.Driver {
	if http, ok := ctx.(contractshttp.Context); ok {
		ctx = http.Context()
	}

	store := *r
	store.ctx = ctx

	return &store
}

func (r *Cache) get(key string) (string, error) {
	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return "", err
	}

	stored, err := kvGet(r.ctx, r.instance, qualified)
	if err != nil {
		return "", err
	}

	return decodeValue(stored)
}

func (r *Cache) pull(key string) (string, error) {
	qualified, err := r.namespace.qualify(r.ctx, key)
	if err != nil {
		return "", err
	}

	stored, found, err := r.instance.GetDel(r.ctx, []byte(qualified))
	if err != nil {
		return "", err
	}
	if !found {
		return "", client.ErrNotFound
	}

	return decodeValue(stored)
}

// lockKey is outside the flush generation, so Flush in epoch mode does not
// free locks other processes hold. "#" keeps it apart from cache keys.
func (r *Cache) lockKey(name string) string {
	return r.prefix + "#lock:" + name
}

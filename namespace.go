package rostam

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/rostamlabs/rostam/client"
)

const (
	FlushEpoch       = "epoch"
	FlushServer      = "server"
	FlushUnsupported = "unsupported"
)

// namespace decides the stored key for a cache key, and what Flush does.
//
// Rostam has no SCAN, no KEYS and no prefix delete, and its flush op wipes the
// entire keyspace of every shard. So the default store puts a generation
// number into every key and flushes by bumping it: the old keys become
// unreachable and age out under their TTLs or the server's eviction.
type namespace interface {
	qualify(ctx context.Context, key string) (string, error)
	flush(ctx context.Context) error
}

func newNamespace(mode, store, prefix string, instance *client.Client, connection string, epochRefresh time.Duration) (namespace, error) {
	switch mode {
	case FlushEpoch, "":
		return &epochNamespace{
			prefix:     prefix,
			generation: sharedGeneration(instance, connection, prefix+"#epoch", epochRefresh),
		}, nil
	case FlushServer:
		return &serverNamespace{prefix: prefix, instance: instance}, nil
	case FlushUnsupported:
		return &staticNamespace{prefix: prefix}, nil
	default:
		return nil, ErrUnknownFlushMode.Args(mode, store)
	}
}

func prefixed(prefix, key string) string {
	if prefix == "" {
		return key
	}

	return prefix + ":" + key
}

// epochNamespace stores keys as "<prefix>:<generation>:<key>".
type epochNamespace struct {
	prefix     string
	generation *generation
}

func (r *epochNamespace) qualify(ctx context.Context, key string) (string, error) {
	current, err := r.generation.current(ctx)
	if err != nil {
		return "", err
	}

	return prefixed(r.prefix, strconv.FormatInt(current, 10)+":"+key), nil
}

func (r *epochNamespace) flush(ctx context.Context) error {
	return r.generation.bump(ctx)
}

// serverNamespace stores keys as "<prefix>:<key>" and flushes the whole
// server: every other prefix, every session and every lock on it go too.
type serverNamespace struct {
	prefix   string
	instance *client.Client
}

func (r *serverNamespace) qualify(_ context.Context, key string) (string, error) {
	return prefixed(r.prefix, key), nil
}

func (r *serverNamespace) flush(ctx context.Context) error {
	return r.instance.Flush(ctx)
}

// staticNamespace stores keys as "<prefix>:<key>" and refuses to flush.
type staticNamespace struct {
	prefix string
}

func (r *staticNamespace) qualify(_ context.Context, key string) (string, error) {
	return prefixed(r.prefix, key), nil
}

var errFlushUnsupported = errors.New("rostam: flush is disabled for this store")

func (r *staticNamespace) flush(context.Context) error {
	return errFlushUnsupported
}

// Generations are shared by every Cache on the same connection and prefix,
// so WithContext copies and other stores of the same prefix agree on the
// current one inside a process.
var generations sync.Map

func sharedGeneration(instance *client.Client, connection, key string, refresh time.Duration) *generation {
	fresh := &generation{instance: instance, key: key, refresh: refresh}
	existing, _ := generations.LoadOrStore(connection+"\x00"+key, fresh)

	return existing.(*generation)
}

// generation caches a counter stored on the server.
//
// refresh bounds how stale the cached number may be: a flush in another
// process is seen at most refresh later (0 re-reads on every call, a negative
// value never re-reads). The number never goes backwards inside a process:
// if the server's copy comes back lower than one this process has seen, the
// counter was evicted or wiped, and it is pushed back up rather than reused,
// because reusing an old generation would resurrect the keys a flush retired.
type generation struct {
	instance *client.Client
	key      string
	refresh  time.Duration

	mu       sync.Mutex
	loaded   bool
	loadedAt time.Time
	value    int64
}

func (r *generation) current(ctx context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.loaded && (r.refresh < 0 || (r.refresh > 0 && time.Since(r.loadedAt) < r.refresh)) {
		return r.value, nil
	}

	stored, err := r.read(ctx)
	if err != nil {
		return 0, err
	}

	if r.loaded && stored < r.value {
		stored, err = r.instance.IncrEx(ctx, []byte(r.key), r.value-stored, 0)
		if err != nil {
			return 0, err
		}
	}

	r.remember(stored)

	return r.value, nil
}

func (r *generation) bump(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	bumped, err := r.instance.IncrEx(ctx, []byte(r.key), 1, 0)
	if err != nil {
		return err
	}

	if r.loaded && bumped <= r.value {
		bumped, err = r.instance.IncrEx(ctx, []byte(r.key), r.value+1-bumped, 0)
		if err != nil {
			return err
		}
	}

	r.remember(bumped)

	return nil
}

func (r *generation) read(ctx context.Context) (int64, error) {
	stored, err := kvGet(ctx, r.instance, r.key)
	if errors.Is(err, client.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if len(stored) != counterSize {
		return 0, ErrGenerationNotACounter.Args(r.key)
	}

	return decodeCounter(stored), nil
}

func (r *generation) remember(value int64) {
	if !r.loaded || value > r.value {
		r.value = value
	}
	r.loaded = true
	r.loadedAt = time.Now()
}

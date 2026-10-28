# Goravel cache and session drivers for Rostam

A [Goravel](https://www.goravel.dev) cache store and session driver backed by
[Rostam](https://github.com/rostamlabs/rostam)'s key-value engine, used the same
way as `goravel/redis`: `facades.Cache().Store("rostam")`, atomic `Add`,
counters, locks, and sessions that survive a cache flush.

It is a thin layer over Rostam's official Go client,
[`github.com/rostamlabs/rostam/client`](https://github.com/rostamlabs/rostam/tree/main/client),
which speaks the native binary TCP protocol (Rostam's key-value half has no
REST surface). Pooling, pipelining, TLS, token auth and NotLeader redirects
come from that client.

## Requirements

- Go **1.26.5** or newer. The Rostam client requires it, even though Goravel
  v1.18 itself allows Go 1.25.
- Goravel **v1.18**
- **Rostam v0.6.0** or newer, started with a `-tcp` listener. The conformance
  suite runs against the latest stable and the latest pre-release.

```bash
rostam-server -tcp 127.0.0.1:7000 -api-key "$(openssl rand -hex 32)" -data /var/lib/rostam
```

## Install

```bash
go get github.com/rostamlabs/rostam-goravel
go run github.com/rostamlabs/rostam-goravel/setup install
```

The installer:

- registers `&rostam.ServiceProvider{}`
- adds a `rostam` connection to `config/database.go`
- adds a `rostam` store to `config/cache.go` and a `rostam` driver to
  `config/session.go`, and makes both the default
- appends the `ROSTAM_*` variables to `.env` and `.env.example`

`go run github.com/rostamlabs/rostam-goravel/setup uninstall` reverses it. It
hands a default back to the framework's only while it still names `rostam`, and
removes the `ROSTAM_*` block only while it is unchanged, so values you have
filled in stay.

To configure it by hand instead, register the provider first. Without it the
facades below answer `ErrServiceProviderNotRegistered`:

```go
// bootstrap/providers.go (or the providers list in config/app.go)
import "github.com/rostamlabs/rostam-goravel"

&rostam.ServiceProvider{},
```

then the connection, the store and the session driver:

```go
// config/database.go
"rostam": map[string]any{
    "default": map[string]any{
        "host":  config.Env("ROSTAM_HOST", ""),
        "port":  config.Env("ROSTAM_PORT", 7000),
        "token": config.Env("ROSTAM_TOKEN", ""),
    },
},

// config/cache.go, under "stores"
"rostam": map[string]any{
    "driver":     "custom",
    "connection": "default",
    "via": func() (cache.Driver, error) {
        return rostamfacades.Cache("rostam") // the key of this store
    },
},

// config/session.go, under "drivers"
"rostam": map[string]any{
    "driver":     "custom",
    "connection": "default",
    "via": func() (session.Driver, error) {
        return rostamfacades.Session("rostam") // the key of this driver
    },
},
```

with `rostamfacades "github.com/rostamlabs/rostam-goravel/facades"`.

## Usage

There is nothing new to learn; it is a cache store:

```go
cache := facades.Cache().Store("rostam")

cache.Put("user:1", "Goravel", time.Hour)
cache.Get("user:1")                          // "Goravel"
cache.Remember("report", 10*time.Minute, buildReport)

cache.Add("job:1:claimed", 1, 5*time.Minute) // atomic: exactly one caller wins
cache.Increment("hits")                      // atomic on the server, keeps its TTL
cache.Pull("one-shot-token")                 // atomic get-and-delete

cache.Lock("deploy", 30*time.Second).Get(deploy)
```

For anything the cache contract does not cover, such as vector search,
fixed-window counters or compare-and-swap, use the client directly:

```go
instance, _ := rostamfacades.Instance("default") // *client.Client
instance.IncrEx(ctx, []byte("rate:ip:1.2.3.4"), 1, time.Minute)
```

## Configuration

`database.rostam.<connection>`:

| key | default | |
| --- | --- | --- |
| `host`, `port` | —, `7000` | a single node's `-tcp` address |
| `servers` | — | `[]string` of `host:port`, for several nodes; overrides `host`/`port` |
| `token` | `""` | the server's `-api-key` |
| `routed` | `true` | shard-aware routing (`client.NewRouted`); `false` uses `client.New` |
| `dial_timeout`, `call_timeout` | `5s` | `time.Duration` |
| `max_conns` | `max(4, NumCPU)` | connections per server |
| `pipeline_depth` | `0` (off) | requests in flight per pipelined connection |
| `tls` | — | a `*tls.Config` |

`cache.stores.<store>`:

| key | default | |
| --- | --- | --- |
| `connection` | `"default"` | |
| `prefix` | `cache.prefix` | |
| `flush` | `"epoch"` | `epoch`, `server` or `unsupported`; see [Flushing](#flushing) |
| `epoch_refresh` | `10s` | `time.Duration`; how stale the cached flush generation may be. `0` re-reads it on every call, a negative value never does |

`session.drivers.<driver>`: `connection` (default `"default"`) and `prefix`
(default `"<session.cookie>:"`). The expiry is `session.lifetime`, which must
be at least one minute.

## How it maps onto Rostam

### Values are strings, integers are counters

`Get` returns what goravel/redis returns: the value's string form (`"5"`,
`"1"` for `true`, `"Goravel"`), and the typed getters parse it. Values are
stored the way go-redis would write them. Types go-redis refuses, such as a
struct that is not an `encoding.BinaryMarshaler`, make `Put` return an error.

The server's `incr_ex` adds only to a value that is exactly eight bytes, read
as a big-endian int64. So integers, and only integers, are stored bare in that
form, and everything else sits behind a one-byte header. A value that would come
out at exactly eight bytes gets a padded header instead, so a 7-byte string can
never be mistaken for a number. This layout is the PHP driver's, but the
payload is not: Go stores strings where PHP stores `serialize()`d values, so
only counters can be read from the other language.

`Increment` is therefore one atomic server operation. It keeps the key's
expiry, like Redis `INCRBY`, and a missing key is created with no expiry. A key
holding a numeric string, for example from `Put("k", "5")`, is converted to a
counter once, with compare-and-swap and its remaining TTL kept. Any other
value returns `ErrNotACounter`.

### Flushing

Rostam has no `FLUSHDB`, `KEYS` or `SCAN`. Its `flush` op wipes the entire
keyspace of every shard, including every other prefix, every session and every
lock. So the default `epoch` mode does what the PHP driver does: every key
carries a generation number (`<prefix>:<generation>:<key>`), and `Flush()`
increments the counter at `<prefix>#epoch`. The old keys become unreachable at
once and leave under their TTLs or the server's eviction.

- Processes cache the generation for `epoch_refresh`, each store with its own
  setting, so a flush made elsewhere is seen at most that long afterwards.
  Inside one process it is seen at once.
- A generation is never reused, because reusing one would make everything it
  retired reachable again. The counter can still be evicted, or wiped by a
  `flush: "server"` store, so it is never created from zero: a missing counter
  is created from the clock in microseconds, and a flush adds one. Every
  generation used so far sits within a flush count of the moment its counter
  was created, so a recreated counter starts past all of them. The one
  assumption is that flushes do not outrun the clock, at more than one per
  microsecond sustained, and a round trip per flush rules that out. A process
  that saw a higher number before the counter was recreated also pushes it back
  up rather than taking the lower one.
- Sessions and locks are not under the generation, so a cache flush neither logs
  anyone out nor frees a lock somebody holds.

`flush: "server"` calls the server's `flush`: everything on that server goes,
and only a server dedicated to this one store should use it.
`flush: "unsupported"` makes `Flush()` return `false`.

goravel/redis flushes with `FLUSHALL`; the default here is deliberately narrower.

### Locks have owners

The framework's own lock, which goravel/redis uses, releases with a plain delete.
A process whose lock already expired therefore deletes the next holder's lock.
These locks store a random owner token with `set_nx` and release with
compare-and-delete (`cad`), so `Release` frees only a lock you still hold. The
`*rostam.Lock` behind the contract also has:

```go
lock := rostam.NewLock(store, "deploy", 30*time.Second)
lock.Extend(time.Minute)                    // compare-and-expire; false once lost
store.RestoreLock("deploy", lock.Owner())   // release it from another process
```

`Block` polls once per second, like the framework, and gives up early when the
store's context is cancelled.

### Durations

Rostam stores milliseconds, and an expiry of zero means "never". So a positive
duration under 1ms is rounded up to 1ms rather than becoming permanent, `0`
means no expiry (as in goravel/redis), and a negative duration on `Put` removes
the key.

## What eviction costs you

A single-node `rostam-server` **evicts rather than refusing a write**, and every
write still answers success. Eviction is by write order, not LRU: reading a key
does not keep it alive, and a key that waits while other writes churn past it is
dropped when the buffer wraps. Only `-cluster` replicated shards refuse writes
instead. `-relocating-eviction` makes the single-node server copy live records
forward, which helps but guarantees nothing. On v0.7.0-beta3 and newer,
`rostam_kv_evictions_live_total` in `__kv_metrics__` counts how many live records
were displaced.

For a cache that is a miss rate. Sessions on the same server inherit it, so a
server that evicts can log people out. Give sessions a server with headroom, or
accept it.

## How large a value can be

The key and the value together must fit in one page of the server's cache, which
is `floorPow2(max_memory / shards / 16)` clamped to 1 MiB…1 GiB. On a default
server that is about 1,048,546 bytes. The key counted is the stored key, prefix
and generation included. A larger entry fails with the server's generic
"internal error".

## Testing

The tests talk to a real `rostam-server`; there is no fake. Without one they
skip:

```bash
rostam-server -tcp 127.0.0.1:7411 -insecure
ROSTAM_TEST_SERVER=127.0.0.1:7411 ROSTAM_TEST_SERVER_IS_DISPOSABLE=1 go test -race ./...
```

`ROSTAM_TEST_SERVER_IS_DISPOSABLE=1` allows the tests that wipe the whole server.
`ROSTAM_TEST_SERVER_TOKEN` passes a token. `ROSTAM_TEST_DOCKER=1` also runs the
container test against `ghcr.io/rostamlabs/rostam`.

`Docker()` implements Goravel's test-container contract on that image, so
`facades.Testing().Docker().Cache("rostam")` works in an application's tests.

## Not included

A queue driver. Rostam's eviction, and the fact that `-cluster` serves reads from
any replica, both turn a lost record into "job finished" in a lease-based queue.
`rostamlabs/rostam-queue-laravel` documents what it takes. `BindingQueue` is
reserved for when that work is done.

## License

Apache-2.0

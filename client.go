package rostam

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/goravel/framework/contracts/config"
	"github.com/rostamlabs/rostam/client"
	"github.com/rostamlabs/rostam/sdk/wire"
)

// Global map of Rostam clients, keyed by the connection name in config.
// A client owns its connection pools, so every store and session driver on
// the same connection shares one.
var (
	clients sync.Map
	mu      sync.Mutex
)

// GetClient returns the Rostam client for the given connection, creating and
// caching it on first use. Creating a client does not dial: pools open lazily
// on the first call, so an unreachable server surfaces on the first operation.
func GetClient(config config.Config, connection string) (*client.Client, error) {
	if instance, ok := clients.Load(connection); ok {
		return instance.(*client.Client), nil
	}

	mu.Lock()
	defer mu.Unlock()

	if instance, ok := clients.Load(connection); ok {
		return instance.(*client.Client), nil
	}

	instance, err := createClient(config, connection)
	if err != nil {
		return nil, err
	}

	clients.Store(connection, instance)

	return instance, nil
}

// createClient reads database.rostam.<connection>:
//
//	servers         []string       every node's -tcp address; overrides host/port
//	host, port      string, int    a single node (port defaults to 7000)
//	token           string         the server's API key
//	routed          bool           shard-aware routing (default true)
//	dial_timeout    time.Duration  default 5s
//	call_timeout    time.Duration  per call when the context has no deadline, default 5s
//	max_conns       int            per server, default max(4, NumCPU)
//	pipeline_depth  int            0 (default) disables pipelining
//	tls             *tls.Config
func createClient(config config.Config, connection string) (*client.Client, error) {
	configPrefix := fmt.Sprintf("database.rostam.%s", connection)

	servers := config.GetStringSlice(configPrefix + ".servers")
	if len(servers) == 0 {
		host := config.GetString(configPrefix + ".host")
		if host == "" {
			return nil, ErrServersNotConfigured.Args(connection, configPrefix, configPrefix)
		}
		servers = []string{net.JoinHostPort(host, config.GetString(configPrefix+".port", "7000"))}
	}

	options := client.Config{
		Servers:           servers,
		AuthToken:         config.GetString(configPrefix + ".token"),
		DialTimeout:       config.GetDuration(configPrefix + ".dial_timeout"),
		CallTimeout:       config.GetDuration(configPrefix + ".call_timeout"),
		MaxConnsPerServer: int32(config.GetInt(configPrefix + ".max_conns")),      //nolint:gosec // a pool size
		PipelineDepth:     int32(config.GetInt(configPrefix + ".pipeline_depth")), //nolint:gosec // a queue depth
	}

	if tlsConfig, ok := config.Get(configPrefix + ".tls").(*tls.Config); ok && tlsConfig != nil {
		options.TLSConfig = tlsConfig
	}

	if config.GetBool(configPrefix+".routed", true) {
		return client.NewRouted(options)
	}

	return client.New(options)
}

// The Go client exposes the conditional ops directly; the plain key-value
// ones go through Call with the wire encoders.

func kvGet(ctx context.Context, instance *client.Client, key string) ([]byte, error) {
	return instance.Call(ctx, "get", wire.EncodeKeyArgs([]byte(key)))
}

func kvPut(ctx context.Context, instance *client.Client, key string, value []byte, ttl time.Duration) error {
	_, err := instance.Call(ctx, "put", wire.EncodePutArgs([]byte(key), value, ttl))

	return err
}

func kvDel(ctx context.Context, instance *client.Client, key string) error {
	_, err := instance.Call(ctx, "del", wire.EncodeKeyArgs([]byte(key)))
	if errors.Is(err, client.ErrNotFound) {
		return nil
	}

	return err
}

// wireTTL converts a Goravel duration to what the server stores. Zero means
// "no expiry" on the wire, so a positive duration shorter than the server's
// millisecond resolution is rounded up instead of silently becoming forever.
// Callers handle negative durations before getting here.
func wireTTL(t time.Duration) time.Duration {
	if t > 0 && t < time.Millisecond {
		return time.Millisecond
	}

	return t
}

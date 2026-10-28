package rostam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/goravel/framework/contracts/config"
	"github.com/goravel/framework/contracts/session"
	"github.com/rostamlabs/rostam/client"
)

var _ session.Driver = (*Session)(nil)

// Session stores session payloads as plain strings under "<prefix><id>",
// with session.lifetime as their expiry. It has no flush generation, so a
// cache Flush in epoch mode never logs anyone out; a cache store with
// flush=server on the same server still wipes sessions.
type Session struct {
	ctx      context.Context
	config   config.Config
	instance *client.Client
	prefix   string
	lifetime time.Duration
}

// NewSession reads session.drivers.<driver>.connection (default "default")
// and .prefix (default "<session.cookie>:").
func NewSession(ctx context.Context, config config.Config, driver string) (*Session, error) {
	driverPrefix := fmt.Sprintf("session.drivers.%s", driver)
	connection := config.GetString(driverPrefix+".connection", "default")

	instance, err := GetClient(config, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to init rostam client: %w", err)
	}

	// Zero would mean "never expires" on the server, so it is refused rather
	// than turned into immortal sessions.
	lifetimeMinutes := config.GetInt("session.lifetime", 120)
	if lifetimeMinutes < 1 {
		return nil, ErrSessionLifetimeTooShort.Args(lifetimeMinutes)
	}

	prefix := config.GetString(driverPrefix+".prefix", config.GetString("session.cookie", "goravel_session")+":")

	return &Session{
		ctx:      ctx,
		config:   config,
		instance: instance,
		prefix:   prefix,
		lifetime: time.Duration(lifetimeMinutes) * time.Minute,
	}, nil
}

// Close does not close the client: other stores on the same connection share it.
func (r *Session) Close() error {
	return nil
}

func (r *Session) Destroy(id string) error {
	if err := kvDel(r.ctx, r.instance, r.key(id)); err != nil {
		return fmt.Errorf("failed to delete session '%s' from rostam: %w", r.key(id), err)
	}

	return nil
}

// Gc does nothing: every write carries the lifetime as its expiry.
func (r *Session) Gc(maxLifetime int) error {
	return nil
}

func (r *Session) Open(path string, name string) error {
	return nil
}

func (r *Session) Read(id string) (string, error) {
	data, err := kvGet(r.ctx, r.instance, r.key(id))
	if errors.Is(err, client.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read session '%s' from rostam: %w", r.key(id), err)
	}

	return string(data), nil
}

// Write saves the payload and restarts its lifetime.
func (r *Session) Write(id string, data string) error {
	if err := kvPut(r.ctx, r.instance, r.key(id), []byte(data), r.lifetime); err != nil {
		return fmt.Errorf("failed to write session '%s' to rostam: %w", r.key(id), err)
	}

	return nil
}

func (r *Session) key(id string) string {
	return r.prefix + id
}

package rostam

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"testing"
	"time"

	"github.com/rostamlabs/rostam/client"
	"github.com/spf13/cast"
)

// testConfig is a flat, dotted-key config.Config, so tests set only the keys
// they care about instead of mocking every read.
type testConfig struct {
	values map[string]any
}

func (r *testConfig) Env(string, ...any) any                    { return nil }
func (r *testConfig) EnvString(string, ...string) string        { return "" }
func (r *testConfig) EnvBool(string, ...bool) bool              { return false }
func (r *testConfig) Add(name string, configuration any)        { r.values[name] = configuration }
func (r *testConfig) UnmarshalKey(key string, rawVal any) error { return nil }

func (r *testConfig) Get(path string, defaultValue ...any) any {
	if value, ok := r.values[path]; ok {
		return value
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

func (r *testConfig) GetString(path string, defaultValue ...string) string {
	if value, ok := r.values[path]; ok {
		return cast.ToString(value)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return ""
}

func (r *testConfig) GetInt(path string, defaultValue ...int) int {
	if value, ok := r.values[path]; ok {
		return cast.ToInt(value)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

func (r *testConfig) GetBool(path string, defaultValue ...bool) bool {
	if value, ok := r.values[path]; ok {
		return cast.ToBool(value)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return false
}

func (r *testConfig) GetDuration(path string, defaultValue ...time.Duration) time.Duration {
	if value, ok := r.values[path]; ok {
		return cast.ToDuration(value)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

func (r *testConfig) GetStringSlice(path string, defaultValue ...[]string) []string {
	if value, ok := r.values[path]; ok {
		return cast.ToStringSlice(value)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// testServer returns ROSTAM_TEST_SERVER, or skips: these tests need a real
// rostam-server, because fakes have hidden real behaviour before.
func testServer(t *testing.T) string {
	t.Helper()

	addr := os.Getenv("ROSTAM_TEST_SERVER")
	if addr == "" {
		t.Skip("set ROSTAM_TEST_SERVER=host:port to run tests against a rostam-server")
	}

	return addr
}

// requireDisposableServer skips tests that wipe the whole server.
func requireDisposableServer(t *testing.T) {
	t.Helper()

	if os.Getenv("ROSTAM_TEST_SERVER_IS_DISPOSABLE") != "1" {
		t.Skip("set ROSTAM_TEST_SERVER_IS_DISPOSABLE=1 to run tests that flush the whole server")
	}
}

// newTestConfig points connection "default" at the test server, with a
// fresh prefix so tests never see each other's keys.
func newTestConfig(t *testing.T) *testConfig {
	t.Helper()

	host, port, err := net.SplitHostPort(testServer(t))
	if err != nil {
		t.Fatalf("ROSTAM_TEST_SERVER must be host:port: %v", err)
	}

	resetSharedState()
	t.Cleanup(resetSharedState)

	return &testConfig{values: map[string]any{
		"database.rostam.default.host":  host,
		"database.rostam.default.port":  port,
		"database.rostam.default.token": os.Getenv("ROSTAM_TEST_SERVER_TOKEN"),
		"cache.prefix":                  "rg-" + randomSuffix(),
		"session.cookie":                "rg-session-" + randomSuffix(),
	}}
}

func resetSharedState() {
	clients.Range(func(key, value any) bool {
		_ = value.(*client.Client).Close()
		clients.Delete(key)

		return true
	})
	generations.Clear()
}

func randomSuffix() string {
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)

	return hex.EncodeToString(suffix)
}

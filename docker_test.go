package rostam

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/goravel/framework/process"
	"github.com/goravel/framework/support/env"
	"github.com/stretchr/testify/require"
)

// TestDockerRunsAUsableServer pulls ghcr.io/rostamlabs/rostam, so it runs only
// where ROSTAM_TEST_DOCKER=1 says Docker is available.
func TestDockerRunsAUsableServer(t *testing.T) {
	if env.IsWindows() || os.Getenv("ROSTAM_TEST_DOCKER") != "1" {
		t.Skip("set ROSTAM_TEST_DOCKER=1 on a machine with Docker to run the container test")
	}

	resetSharedState()
	t.Cleanup(resetSharedState)

	config := &testConfig{values: map[string]any{
		"database.rostam.default.host": "127.0.0.1",
		"cache.prefix":                 "rg-docker",
	}}

	docker, err := NewDocker(config, process.New(), testStore)
	require.NoError(t, err)
	require.NoError(t, docker.Build())
	t.Cleanup(func() { _ = docker.Shutdown() })
	require.NoError(t, docker.Ready())

	require.NotEmpty(t, docker.Config().ContainerID)
	require.Equal(t, docker.Config().Port, config.GetInt("database.rostam.default.port"))

	cache, err := NewCache(context.Background(), config, process.New(), testStore)
	require.NoError(t, err)
	require.NoError(t, cache.Put("name", "Goravel", 10*time.Second))
	require.Equal(t, "Goravel", cache.Get("name"))

	_, err = cache.Docker()
	require.NoError(t, err)

	require.NoError(t, docker.Fresh())
	require.False(t, cache.Has("name"))
}

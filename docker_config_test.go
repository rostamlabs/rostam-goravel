package rostam

import (
	"testing"

	contractsdocker "github.com/goravel/framework/contracts/testing/docker"
	"github.com/goravel/framework/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What Docker() makes of a connection's config, checked without starting a
// container.

func TestDockerReachesAServersOnlyConnectionThroughItsFirstNode(t *testing.T) {
	config := &testConfig{values: map[string]any{
		"database.rostam.default.servers": []string{"10.0.0.5:7000", "10.0.0.6:7000"},
	}}

	docker, err := NewDocker(config, process.New(), testStore)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", docker.Config().Host)

	docker.cacheConfig.Port = 49152 // what Build() maps the container to
	docker.resetConfigPort()

	assert.Equal(t, []string{"10.0.0.5:49152"}, config.GetStringSlice("database.rostam.default.servers"),
		"the servers list still pointed at the old nodes, which win over host and port")
}

func TestDockerLeavesAHostConnectionsServersAlone(t *testing.T) {
	config := &testConfig{values: map[string]any{"database.rostam.default.host": "127.0.0.1"}}

	docker, err := NewDocker(config, process.New(), testStore)
	require.NoError(t, err)

	docker.cacheConfig.Port = 49152
	docker.resetConfigPort()

	assert.Equal(t, 49152, config.GetInt("database.rostam.default.port"))
	assert.Nil(t, config.Get("database.rostam.default.servers"))
}

func TestACustomImageIsMappedOnItsOwnPort(t *testing.T) {
	config := &testConfig{values: map[string]any{"database.rostam.default.host": "127.0.0.1"}}

	docker, err := NewDocker(config, process.New(), testStore)
	require.NoError(t, err)
	assert.Equal(t, dockerTCPPort, docker.internalPort)

	for exposed, want := range map[string]int{"8100": 8100, "40000:8200": 8200, "8300/tcp": 8300} {
		docker.Image(contractsdocker.Image{Repository: "example/rostam", Tag: "dev", ExposedPorts: []string{exposed}})
		assert.Equal(t, want, docker.internalPort, exposed)
	}
}

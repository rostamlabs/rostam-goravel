package rostam

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	contractsconfig "github.com/goravel/framework/contracts/config"
	contractsprocess "github.com/goravel/framework/contracts/process"
	contractsdocker "github.com/goravel/framework/contracts/testing/docker"
	supportdocker "github.com/goravel/framework/support/docker"
	testingdocker "github.com/goravel/framework/testing/docker"
	"github.com/rostamlabs/rostam/client"
	"github.com/spf13/cast"
)

const (
	dockerRepository   = "ghcr.io/rostamlabs/rostam"
	dockerTag          = "0.7"
	dockerTCPPort      = 7000
	dockerReadyTimeout = 2 * time.Minute
)

// Docker runs a disposable rostam-server container for tests.
type Docker struct {
	config      contractsconfig.Config
	cacheConfig contractsdocker.CacheConfig
	imageDriver contractsdocker.ImageDriver
	process     contractsprocess.Process
	connection  string
	// internalPort is the TCP port the server listens on inside the
	// container; cacheConfig.Port becomes the host port it is mapped to.
	internalPort int
	// usesServers is set when the connection names its nodes in a servers
	// list, which Ready then points at the container instead of host/port.
	usesServers bool
}

func NewDocker(config contractsconfig.Config, process contractsprocess.Process, store string) (*Docker, error) {
	connection := config.GetString(fmt.Sprintf("cache.stores.%s.connection", store), "default")
	configPrefix := fmt.Sprintf("database.rostam.%s", connection)
	servers := config.GetStringSlice(configPrefix + ".servers")
	host := config.GetString(configPrefix + ".host")
	if host == "" && len(servers) > 0 {
		// The container is reached on the first node's host.
		if splitHost, _, err := net.SplitHostPort(servers[0]); err == nil {
			host = splitHost
		}
	}
	if host == "" {
		return nil, ErrServersNotConfigured.Args(connection, configPrefix, configPrefix)
	}

	token := config.GetString(configPrefix + ".token")

	// The image's default command also serves HTTP; tests only need the TCP
	// listener. A server on a non-loopback address without a key refuses to
	// start unless told to run open.
	args := []string{"-tcp", fmt.Sprintf("0.0.0.0:%d", dockerTCPPort)}
	if token == "" {
		args = append(args, "-insecure")
	} else {
		args = append(args, "-api-key", token)
	}

	return &Docker{
		connection: connection,
		config:     config,
		cacheConfig: contractsdocker.CacheConfig{
			Host:     host,
			Password: token,
			Port:     dockerTCPPort,
		},
		imageDriver: testingdocker.NewImageDriver(contractsdocker.Image{
			Repository:   dockerRepository,
			Tag:          dockerTag,
			ExposedPorts: []string{strconv.Itoa(dockerTCPPort)},
			Args:         args,
		}, process),
		process:      process,
		internalPort: dockerTCPPort,
		usesServers:  len(servers) > 0,
	}, nil
}

func (r *Docker) Build() error {
	if err := r.imageDriver.Build(); err != nil {
		return err
	}

	config := r.imageDriver.Config()
	r.cacheConfig.ContainerID = config.ContainerID
	r.cacheConfig.Port = cast.ToInt(supportdocker.ExposedPort(config.ExposedPorts, strconv.Itoa(r.internalPort)))

	return nil
}

func (r *Docker) Config() contractsdocker.CacheConfig {
	return r.cacheConfig
}

// Fresh wipes the container's whole keyspace.
func (r *Docker) Fresh() error {
	instance, err := r.connect()
	if err != nil {
		return fmt.Errorf("connect Rostam docker error: %v", err)
	}
	defer func() { _ = instance.Close() }()

	if err := instance.Flush(context.Background()); err != nil {
		return fmt.Errorf("fresh Rostam docker error: %v", err)
	}

	return nil
}

// Image swaps the image the container is built from. Its first exposed port
// - "7000", or "host:container" - is the one the server listens on.
func (r *Docker) Image(image contractsdocker.Image) {
	r.imageDriver = testingdocker.NewImageDriver(image, r.process)

	if len(image.ExposedPorts) > 0 {
		exposed := image.ExposedPorts[0]
		if i := strings.LastIndex(exposed, ":"); i >= 0 {
			exposed = exposed[i+1:]
		}
		if port, err := strconv.Atoi(strings.SplitN(exposed, "/", 2)[0]); err == nil {
			r.internalPort = port
		}
	}
}

func (r *Docker) Ready() error {
	instance, err := r.connect()
	if err != nil {
		return fmt.Errorf("connect Rostam docker error: %v", err)
	}

	r.resetConfigPort()

	return instance.Close()
}

func (r *Docker) Reuse(containerID string, port int) error {
	r.cacheConfig.ContainerID = containerID
	r.cacheConfig.Port = port

	return nil
}

func (r *Docker) Shutdown() error {
	return r.imageDriver.Shutdown()
}

// connect waits up to two minutes in all for the server to answer: each
// attempt and each pause between attempts is cut short by the same deadline.
func (r *Docker) connect() (*client.Client, error) {
	instance, err := client.New(client.Config{
		Servers:   []string{net.JoinHostPort(r.cacheConfig.Host, strconv.Itoa(r.cacheConfig.Port))},
		AuthToken: r.cacheConfig.Password,
	})
	if err != nil {
		return nil, err
	}

	deadline, cancel := context.WithTimeout(context.Background(), dockerReadyTimeout)
	defer cancel()

	for {
		attempt, cancelAttempt := context.WithTimeout(deadline, 2*time.Second)
		_, err = instance.Exists(attempt, []byte("rostam-goravel:ready"))
		cancelAttempt()
		if err == nil {
			return instance, nil
		}

		select {
		case <-deadline.Done():
			_ = instance.Close()

			return nil, err
		case <-time.After(2 * time.Second):
		}
	}
}

// resetConfigPort points the connection at the container: its port, and its
// servers list when the connection names its nodes that way - otherwise the
// list would still win over host and port, and tests would reach the old
// server rather than the disposable one.
func (r *Docker) resetConfigPort() {
	r.config.Add(fmt.Sprintf("database.rostam.%s.port", r.connection), r.cacheConfig.Port)

	if r.usesServers {
		r.config.Add(
			fmt.Sprintf("database.rostam.%s.servers", r.connection),
			[]string{net.JoinHostPort(r.cacheConfig.Host, strconv.Itoa(r.cacheConfig.Port))},
		)
	}
}

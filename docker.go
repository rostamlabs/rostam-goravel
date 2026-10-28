package rostam

import (
	"context"
	"fmt"
	"net"
	"strconv"
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
	dockerRepository = "ghcr.io/rostamlabs/rostam"
	dockerTag        = "0.7"
	dockerTCPPort    = 7000
)

// Docker runs a disposable rostam-server container for tests.
type Docker struct {
	config      contractsconfig.Config
	cacheConfig contractsdocker.CacheConfig
	imageDriver contractsdocker.ImageDriver
	process     contractsprocess.Process
	connection  string
}

func NewDocker(config contractsconfig.Config, process contractsprocess.Process, store string) (*Docker, error) {
	connection := config.GetString(fmt.Sprintf("cache.stores.%s.connection", store), "default")
	configPrefix := fmt.Sprintf("database.rostam.%s", connection)
	host := config.GetString(configPrefix + ".host")
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
		process: process,
	}, nil
}

func (r *Docker) Build() error {
	if err := r.imageDriver.Build(); err != nil {
		return err
	}

	config := r.imageDriver.Config()
	r.cacheConfig.ContainerID = config.ContainerID
	r.cacheConfig.Port = cast.ToInt(supportdocker.ExposedPort(config.ExposedPorts, strconv.Itoa(r.cacheConfig.Port)))

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

func (r *Docker) Image(image contractsdocker.Image) {
	r.imageDriver = testingdocker.NewImageDriver(image, r.process)
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

// connect waits up to two minutes for the server to answer.
func (r *Docker) connect() (*client.Client, error) {
	instance, err := client.New(client.Config{
		Servers:   []string{net.JoinHostPort(r.cacheConfig.Host, strconv.Itoa(r.cacheConfig.Port))},
		AuthToken: r.cacheConfig.Password,
	})
	if err != nil {
		return nil, err
	}

	for range 60 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err = instance.Exists(ctx, []byte("rostam-goravel:ready"))
		cancel()
		if err == nil {
			return instance, nil
		}

		time.Sleep(2 * time.Second)
	}

	_ = instance.Close()

	return nil, err
}

func (r *Docker) resetConfigPort() {
	r.config.Add(fmt.Sprintf("database.rostam.%s.port", r.connection), r.cacheConfig.Port)
}

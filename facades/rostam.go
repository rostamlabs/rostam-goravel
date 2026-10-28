package facades

import (
	"github.com/goravel/framework/contracts/cache"
	"github.com/goravel/framework/contracts/session"
	"github.com/goravel/framework/errors"
	"github.com/rostamlabs/rostam/client"

	"github.com/rostamlabs/rostam-goravel"
)

func Cache(store string) (cache.Driver, error) {
	if rostam.App == nil {
		return nil, rostam.ErrServiceProviderNotRegistered
	}
	if store == "" {
		return nil, rostam.ErrStoreIsRequired
	}

	instance, err := rostam.App.MakeWith(rostam.BindingCache, map[string]any{"store": store})
	if err != nil {
		return nil, err
	}

	return instance.(*rostam.Cache), nil
}

func Session(driver string) (session.Driver, error) {
	if rostam.App == nil {
		return nil, rostam.ErrServiceProviderNotRegistered
	}
	if driver == "" {
		return nil, rostam.ErrSessionDriverIsRequired
	}

	instance, err := rostam.App.MakeWith(rostam.BindingSession, map[string]any{"driver": driver})
	if err != nil {
		return nil, err
	}

	return instance.(*rostam.Session), nil
}

// Instance returns the Rostam client of a connection, for operations the
// cache contract does not cover (vector search, rate-limit windows, ...).
func Instance(connection string) (*client.Client, error) {
	if rostam.App == nil {
		return nil, rostam.ErrServiceProviderNotRegistered
	}
	if connection == "" {
		return nil, rostam.ErrConnectionIsRequired
	}

	config := rostam.App.MakeConfig()
	if config == nil {
		return nil, errors.ConfigFacadeNotSet.SetModule("Rostam")
	}

	return rostam.GetClient(config, connection)
}

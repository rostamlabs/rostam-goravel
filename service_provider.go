package rostam

import (
	"context"

	"github.com/goravel/framework/contracts/binding"
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/errors"
)

const (
	BindingCache   = "goravel.rostam.cache"
	BindingSession = "goravel.rostam.session"
	// BindingQueue is reserved for the queue driver, which is not shipped yet.
	BindingQueue = "goravel.rostam.queue"

	Name = "rostam"
)

var App foundation.Application

type ServiceProvider struct {
}

func (r *ServiceProvider) Relationship() binding.Relationship {
	return binding.Relationship{
		Bindings: []string{
			BindingCache,
			BindingSession,
		},
		Dependencies: []string{
			binding.Config,
		},
		ProvideFor: []string{
			binding.Cache,
			binding.Session,
		},
	}
}

func (r *ServiceProvider) Register(app foundation.Application) {
	App = app

	app.BindWith(BindingCache, func(app foundation.Application, parameters map[string]any) (any, error) {
		config := app.MakeConfig()
		if config == nil {
			return nil, errors.ConfigFacadeNotSet.SetModule(errors.ModuleCache)
		}

		return NewCache(context.Background(), config, app.MakeProcess(), parameters["store"].(string))
	})
	app.BindWith(BindingSession, func(app foundation.Application, parameters map[string]any) (any, error) {
		config := app.MakeConfig()
		if config == nil {
			return nil, errors.ConfigFacadeNotSet.SetModule(errors.ModuleSession)
		}

		return NewSession(context.Background(), config, parameters["driver"].(string))
	})
}

func (r *ServiceProvider) Boot(app foundation.Application) {

}

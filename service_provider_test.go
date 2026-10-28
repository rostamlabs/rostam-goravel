package rostam

import (
	"errors"
	"testing"

	"github.com/goravel/framework/contracts/foundation"
	mocksfoundation "github.com/goravel/framework/mocks/foundation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type bindingFunc = func(foundation.Application, map[string]any) (any, error)

// The bindings are reached through App.MakeWith with whatever map a caller
// passes; a missing or mistyped name is an error to return, not a panic.
func TestBindingsRefuseAMissingOrMistypedName(t *testing.T) {
	app := mocksfoundation.NewApplication(t)
	bindings := map[any]bindingFunc{}

	// Register stores the application in the package-global App; put back
	// whatever was there so no later test sees this mock.
	previous := App
	t.Cleanup(func() { App = previous })

	app.EXPECT().BindWith(mock.Anything, mock.Anything).Run(func(key any, callback func(foundation.Application, map[string]any) (any, error)) {
		bindings[key] = callback
	}).Times(2)

	(&ServiceProvider{}).Register(app)

	app.EXPECT().MakeConfig().Return(&testConfig{values: map[string]any{}})

	for _, parameters := range []map[string]any{{}, {"store": 42}, {"store": ""}} {
		_, err := bindings[BindingCache](app, parameters)
		assert.True(t, errors.Is(err, ErrStoreIsRequired), "%v: %v", parameters, err)
	}

	for _, parameters := range []map[string]any{{}, {"driver": 42}, {"driver": ""}} {
		_, err := bindings[BindingSession](app, parameters)
		assert.True(t, errors.Is(err, ErrSessionDriverIsRequired), "%v: %v", parameters, err)
	}
}

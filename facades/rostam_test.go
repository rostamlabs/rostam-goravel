package facades

import (
	"testing"

	"github.com/goravel/framework/errors"
	mocksfoundation "github.com/goravel/framework/mocks/foundation"
	"github.com/stretchr/testify/assert"

	"github.com/rostamlabs/rostam-goravel"
)

func TestInstanceWithoutAConfigFacadeIsAnErrorNotAPanic(t *testing.T) {
	app := mocksfoundation.NewApplication(t)
	app.EXPECT().MakeConfig().Return(nil).Once()

	previous := rostam.App
	rostam.App = app
	t.Cleanup(func() { rostam.App = previous })

	instance, err := Instance("default")

	assert.Nil(t, instance)
	assert.True(t, errors.Is(err, errors.ConfigFacadeNotSet), "%v", err)
}

func TestFacadesWithoutTheProviderSaySo(t *testing.T) {
	previous := rostam.App
	rostam.App = nil
	t.Cleanup(func() { rostam.App = previous })

	_, err := Instance("default")
	assert.True(t, errors.Is(err, rostam.ErrServiceProviderNotRegistered))

	_, err = Cache("rostam")
	assert.True(t, errors.Is(err, rostam.ErrServiceProviderNotRegistered))
}

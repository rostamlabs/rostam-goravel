package main

import (
	"os"

	"github.com/goravel/framework/packages"
	"github.com/goravel/framework/packages/match"
	"github.com/goravel/framework/packages/modify"
	"github.com/goravel/framework/support/env"
	"github.com/goravel/framework/support/file"
	"github.com/goravel/framework/support/path"
	supportstubs "github.com/goravel/framework/support/stubs"
)

func main() {
	setup := packages.Setup(os.Args)
	cacheConfig := `map[string]any{
        "driver": "custom",
        "connection": "default",
        "via": func() (cache.Driver, error) {
            return rostamfacades.Cache("rostam") // The ` + "`rostam`" + ` value is the key of ` + "`stores`" + `
        },
    }`
	databaseConfig := `map[string]any{
        "default": map[string]any{
            "host":  config.Env("ROSTAM_HOST", ""),
            "port":  config.Env("ROSTAM_PORT", 7000),
            "token": config.Env("ROSTAM_TOKEN", ""),
        },
    }`
	sessionConfig := `map[string]any{
        "driver": "custom",
        "connection": "default",
        "via": func() (session.Driver, error) {
            return rostamfacades.Session("rostam") // The ` + "`rostam`" + ` value is the key of ` + "`drivers`" + `
        },
    }`

	appConfigPath := path.Config("app.go")
	databaseConfigPath := path.Config("database.go")
	cacheConfigPath := path.Config("cache.go")
	sessionConfigPath := path.Config("session.go")
	rostamServiceProvider := "&rostam.ServiceProvider{}"
	moduleImport := setup.Paths().Module().Import()
	configPackage := setup.Paths().Config().Package()
	facadesImport := setup.Paths().Facades().Import()
	facadesPackage := setup.Paths().Facades().Package()
	envPath := path.Base(".env")
	envExamplePath := path.Base(".env.example")
	cacheContract := "github.com/goravel/framework/contracts/cache"
	sessionContract := "github.com/goravel/framework/contracts/session"
	rostamFacades := "github.com/rostamlabs/rostam-goravel/facades"
	envContent := `
ROSTAM_HOST=127.0.0.1
ROSTAM_PORT=7000
ROSTAM_TOKEN=
`

	setup.Install(
		// Add the rostam service provider to app.go if not using bootstrap setup
		modify.When(func(_ map[string]any) bool {
			return !env.IsBootstrapSetup()
		}, modify.GoFile(appConfigPath).
			Find(match.Imports()).Modify(modify.AddImport(moduleImport)).
			Find(match.Providers()).Modify(modify.Register(rostamServiceProvider))),

		// Add the rostam service provider to providers.go if using bootstrap setup
		modify.When(func(_ map[string]any) bool {
			return env.IsBootstrapSetup()
		}, modify.RegisterProvider(moduleImport, rostamServiceProvider)),

		// Create config/database.go
		modify.WhenFileNotExists(databaseConfigPath, modify.File(databaseConfigPath).Overwrite(supportstubs.DatabaseConfig(configPackage, facadesImport, facadesPackage))),

		// Add rostam connections to database.go
		modify.GoFile(databaseConfigPath).
			Find(match.Config("database")).Modify(modify.AddConfig("rostam", databaseConfig, "// Rostam connections")),

		// Add the rostam cache store to cache.go if the cache config file exists
		modify.WhenFileExists(cacheConfigPath,
			modify.GoFile(cacheConfigPath).
				Find(match.Imports()).Modify(modify.AddImport(cacheContract), modify.AddImport(rostamFacades, "rostamfacades")).
				Find(match.Config("cache.stores")).Modify(modify.AddConfig("rostam", cacheConfig)).
				Find(match.Config("cache")).Modify(modify.AddConfig("default", `"rostam"`)),
		),

		// Add the rostam session driver to session.go if the session config file exists
		modify.WhenFileExists(sessionConfigPath,
			modify.GoFile(sessionConfigPath).
				Find(match.Imports()).Modify(modify.AddImport(sessionContract), modify.AddImport(rostamFacades, "rostamfacades")).
				Find(match.Config("session.drivers")).Modify(modify.AddConfig("rostam", sessionConfig)).
				Find(match.Config("session")).Modify(modify.AddConfig("default", `"rostam"`)),
		),

		// Add configurations to the .env and .env.example files
		modify.WhenFileNotContains(envPath, "ROSTAM_HOST", modify.File(envPath).Append(envContent)),
		modify.WhenFileNotContains(envExamplePath, "ROSTAM_HOST", modify.File(envExamplePath).Append(envContent)),
	).Uninstall(
		// Remove the rostam service provider from app.go if not using bootstrap setup
		modify.When(func(_ map[string]any) bool {
			return !env.IsBootstrapSetup()
		}, modify.GoFile(appConfigPath).
			Find(match.Providers()).Modify(modify.Unregister(rostamServiceProvider)).
			Find(match.Imports()).Modify(modify.RemoveImport(moduleImport))),

		// Remove the rostam service provider from providers.go if using bootstrap setup
		modify.When(func(_ map[string]any) bool {
			return env.IsBootstrapSetup()
		}, modify.UnregisterProvider(moduleImport, rostamServiceProvider)),

		// Remove the rostam cache store from cache.go if the cache config file exists
		modify.WhenFileExists(cacheConfigPath,
			modify.GoFile(cacheConfigPath).
				Find(match.Config("cache")).Modify(modify.AddConfig("default", `"memory"`)).
				Find(match.Config("cache.stores")).Modify(modify.RemoveConfig("rostam")).
				Find(match.Imports()).Modify(modify.RemoveImport(cacheContract), modify.RemoveImport(rostamFacades, "rostamfacades")),
		),

		// Remove the rostam session driver from session.go if the session config file exists
		modify.WhenFileExists(sessionConfigPath,
			modify.GoFile(sessionConfigPath).
				Find(match.Config("session")).Modify(modify.AddConfig("default", `"file"`)).
				Find(match.Config("session.drivers")).Modify(modify.RemoveConfig("rostam")).
				Find(match.Imports()).Modify(modify.RemoveImport(sessionContract), modify.RemoveImport(rostamFacades, "rostamfacades")),
		),

		// Remove rostam connections from database.go
		modify.GoFile(databaseConfigPath).
			Find(match.Config("database")).Modify(modify.RemoveConfig("rostam")),

		// Remove config/database.go if nothing else was added to it
		modify.When(func(_ map[string]any) bool {
			content, err := file.GetContent(databaseConfigPath)
			if err != nil {
				return false
			}
			return content == supportstubs.DatabaseConfig(configPackage, facadesImport, facadesPackage)
		}, modify.File(databaseConfigPath).Remove()),
	).Execute()
}

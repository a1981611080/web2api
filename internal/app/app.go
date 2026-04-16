package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"web2api/internal/account"
	"web2api/internal/consumer"
	"web2api/internal/httpapi"
	"web2api/internal/modelroute"
	"web2api/internal/plugin"
	"web2api/internal/source"
)

type App struct {
	router        chi.Router
	pluginManager *plugin.Manager
	sourceStore   *source.Store
	accountStore  *account.Store
	consumerStore *consumer.Store
	modelRoutes   *modelroute.Store
}

func New() (*App, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	pluginsDir := filepath.Join(root, "plugins")
	dataDir := filepath.Join(root, "data")
	webDir := filepath.Join(root, "web")

	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	pluginManager, err := plugin.NewManager(pluginsDir)
	if err != nil {
		return nil, err
	}
	if err := pluginManager.Scan(); err != nil {
		return nil, err
	}

	sourceStore, err := source.NewStore(filepath.Join(dataDir, "sources.json"))
	if err != nil {
		return nil, err
	}
	accountStore, err := account.NewStore(filepath.Join(dataDir, "accounts.json"))
	if err != nil {
		return nil, err
	}
	consumerStore, err := consumer.NewStore(filepath.Join(dataDir, "consumers.json"))
	if err != nil {
		return nil, err
	}
	modelRoutes, err := modelroute.NewStore(filepath.Join(dataDir, "model_routes.json"))
	if err != nil {
		return nil, err
	}

	h := httpapi.NewHandler(pluginManager, sourceStore, accountStore, consumerStore, modelRoutes, webDir)

	return &App{
		router:        h.Router(),
		pluginManager: pluginManager,
		sourceStore:   sourceStore,
		accountStore:  accountStore,
		consumerStore: consumerStore,
		modelRoutes:   modelRoutes,
	}, nil
}

func (a *App) Router() http.Handler {
	return a.router
}

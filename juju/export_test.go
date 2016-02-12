package juju

import (
	"github.com/juju/juju/api"
	"github.com/juju/juju/jujuclient"
)

var (
	ProviderConnectDelay   = &providerConnectDelay
	GetConfig              = getConfig
	CacheChangedAPIInfo    = cacheChangedAPIInfo
	CacheAPIInfo           = cacheAPIInfo
	ResolveOrDropHostnames = &resolveOrDropHostnames
	ServerAddress          = &serverAddress
)

func NewAPIFromStore(controllerName, modelName string, store jujuclient.ClientStore, f api.OpenFunc) (api.Connection, error) {
	apiOpen := func(info *api.Info, opts api.DialOpts) (api.Connection, error) {
		return f(info, opts)
	}
	return newAPIFromStore(controllerName, modelName, store, apiOpen, nil)
}

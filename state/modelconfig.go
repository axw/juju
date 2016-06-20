// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"github.com/juju/errors"

	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/environs/config"
)

// ModelConfig returns the complete config for the model represented
// by this state.
func (st *State) ModelConfig() (*config.Config, error) {
	defaultModelSettings, err := readSettings(st, controllersC, defaultModelSettingsGlobalKey)
	if err != nil {
		return nil, errors.Trace(err)
	}
	modelSettings, err := readSettings(st, settingsC, modelGlobalKey)
	if err != nil {
		return nil, errors.Trace(err)
	}
	attrs := defaultModelSettings.Map()

	// Merge in model specific settings.
	for k, v := range modelSettings.Map() {
		attrs[k] = v
	}

	cloud, err := st.Cloud()
	if err != nil {
		return nil, errors.Trace(err)
	}
	model, err := st.Model()
	if err != nil {
		return nil, errors.Trace(err)
	}
	endpoint := cloud.Endpoint
	storageEndpoint := cloud.StorageEndpoint
	regionName := model.CloudRegion()
	if regionName != "" {
		region, err := jujucloud.RegionByName(cloud.Regions, regionName)
		if err != nil {
			return nil, errors.Trace(err)
		}
		endpoint = region.Endpoint
		storageEndpoint = region.StorageEndpoint
	}

	// Add the cloud config.
	attrs[config.CloudKey] = map[string]interface{}{
		"type":             cloud.Type,
		"region":           regionName,
		"endpoint":         endpoint,
		"storage-endpoint": storageEndpoint,
	}

	// Add the cloud credentials.
	credentialName := model.CloudCredential()
	if credentialName != "" {
		// TODO(axw) add helper function for getting a named credential.
		cloudCredentials, err := st.CloudCredentials(model.Owner())
		if err != nil {
			return nil, errors.Trace(err)
		}
		credential, ok := cloudCredentials[credentialName]
		if !ok {
			return nil, errors.NotFoundf("credential %q", credentialName)
		}
		credentialAttrs := credential.Attributes()
		credentialAttrs["auth-type"] = string(credential.AuthType())
		attrs[config.CredentialsKey] = credentialAttrs
	}

	return config.New(config.NoDefaults, attrs)
}

// checkModelConfig returns an error if the config is definitely invalid.
func checkModelConfig(cfg *config.Config) error {
	if cfg.AdminSecret() != "" {
		return errors.Errorf("admin-secret should never be written to the state")
	}
	if _, ok := cfg.AgentVersion(); !ok {
		return errors.Errorf("agent-version must always be set in state")
	}
	return nil
}

// checkModelConfigDefaults returns an error if the shared config is definitely invalid.
func checkModelConfigDefaults(attrs map[string]interface{}) error {
	if _, ok := attrs[config.AdminSecretKey]; ok {
		return errors.Errorf("config defaults cannot contain admin-secret")
	}
	if _, ok := attrs[config.AgentVersionKey]; ok {
		return errors.Errorf("config defaults cannot contain agent-version")
	}
	for _, attrName := range controller.ControllerOnlyConfigAttributes {
		if _, ok := attrs[attrName]; ok {
			return errors.Errorf("config defaults cannot contain controller attribute %q", attrName)
		}
	}
	return nil
}

func (st *State) buildAndValidateModelConfig(updateAttrs map[string]interface{}, removeAttrs []string, oldConfig *config.Config) (validCfg *config.Config, err error) {
	for attr := range updateAttrs {
		if controllerOnlyAttribute(attr) {
			return nil, errors.Errorf("cannot set controller attribute %q on a model", attr)
		}
	}
	newConfig, err := oldConfig.Apply(updateAttrs)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if len(removeAttrs) != 0 {
		newConfig, err = newConfig.Remove(removeAttrs)
		if err != nil {
			return nil, errors.Trace(err)
		}
	}
	if err := checkModelConfig(newConfig); err != nil {
		return nil, errors.Trace(err)
	}
	return st.validate(newConfig, oldConfig)
}

type ValidateConfigFunc func(updateAttrs map[string]interface{}, removeAttrs []string, oldConfig *config.Config) error

// UpdateModelConfig adds, updates or removes attributes in the current
// configuration of the model with the provided updateAttrs and
// removeAttrs.
func (st *State) UpdateModelConfig(updateAttrs map[string]interface{}, removeAttrs []string, additionalValidation ValidateConfigFunc) error {
	if len(updateAttrs)+len(removeAttrs) == 0 {
		return nil
	}

	// TODO(axw) 2013-12-6 #1167616
	// Ensure that the settings on disk have not changed
	// underneath us. The settings changes are actually
	// applied as a delta to what's on disk; if there has
	// been a concurrent update, the change may not be what
	// the user asked for.

	modelSettings, err := readSettings(st, settingsC, modelGlobalKey)
	if err != nil {
		return errors.Trace(err)
	}

	// Get the existing model config from state.
	oldConfig, err := st.ModelConfig()
	if err != nil {
		return errors.Trace(err)
	}
	if additionalValidation != nil {
		err = additionalValidation(updateAttrs, removeAttrs, oldConfig)
		if err != nil {
			return errors.Trace(err)
		}
	}
	validCfg, err := st.buildAndValidateModelConfig(updateAttrs, removeAttrs, oldConfig)
	if err != nil {
		return errors.Trace(err)
	}

	validAttrs := validCfg.AllAttrs()
	for k := range oldConfig.AllAttrs() {
		if _, ok := validAttrs[k]; !ok {
			modelSettings.Delete(k)
		}
	}

	// Remove any attributes that are the same as what's in cloud config.
	// TODO(wallyworld) if/when cloud config becomes mutable, we must check
	// for concurrent changes when writing config to ensure the validation
	// we do here remains true
	defaultAttrs, err := st.ModelConfigDefaults()
	if err != nil {
		return errors.Trace(err)
	}
	for attr, sharedValue := range defaultAttrs {
		if newValue, ok := validAttrs[attr]; ok && newValue == sharedValue {
			delete(validAttrs, attr)
			modelSettings.Delete(attr)
		}
	}

	modelSettings.Update(validAttrs)
	_, err = modelSettings.Write()
	return errors.Trace(err)
}

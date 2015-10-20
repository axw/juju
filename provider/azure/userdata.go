// Copyright 2015 Canonical Ltd.
// Copyright 2015 Cloudbase Solutions SRL
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/juju/errors"
	"github.com/juju/utils"
	"github.com/juju/utils/os"

	"github.com/juju/juju/cloudconfig/providerinit/renderers"
)

const (
	// The userdata on windows will arrive as CustomData.bin
	// We need to execute that as a powershell script and then remove it
	bootstrapUserdataScript = `#ps1_sysnative
mv C:\AzureData\CustomData.bin C:\AzureData\CustomData.ps1
& C:\AzureData\CustomData.ps1
rm C:\AzureData\CustomData.ps1
`
	bootstrapUserdataScriptFilename = "juju-userdata.ps1"
)

type AzureRenderer struct{}

func (AzureRenderer) EncodeUserdata(udata []byte, vers os.OSType) ([]byte, error) {
	switch vers {
	case os.Ubuntu, os.CentOS:
		return renderers.ToBase64(utils.Gzip(udata)), nil
	case os.Windows:
		return renderers.ToBase64(renderers.WinEmbedInScript(udata)), nil
	default:
		return nil, errors.Errorf("Cannot encode userdata for OS: %s", vers)
	}
}

// TODO(axw)
/*
// makeUserdataResourceExtension will upload the userdata to storage and then fill in the proper xml
// following the example here
// https://msdn.microsoft.com/en-us/library/azure/dn781373.aspx
func makeUserdataResourceExtension(nonce string, userData string, snapshot *azureEnviron) (*gwacl.ResourceExtensionReference, error) {
	// The bootstrap userdata script is the same for all machines.
	// So we first check if it's already uploaded and if it isn't we upload it
	_, err := snapshot.storage.Get(bootstrapUserdataScriptFilename)
	if errors.IsNotFound(err) {
		err := snapshot.storage.Put(bootstrapUserdataScriptFilename, bytes.NewReader([]byte(bootstrapUserdataScript)), int64(len(bootstrapUserdataScript)))
		if err != nil {
			logger.Errorf(err.Error())
			return nil, errors.Annotate(err, "cannot upload userdata to storage")
		}
	}

	uri, err := snapshot.storage.URL(bootstrapUserdataScriptFilename)
	if err != nil {
		logger.Errorf(err.Error())
		return nil, errors.Trace(err)
	}

	scriptPublicConfig, err := makeUserdataResourceScripts(uri, bootstrapUserdataScriptFilename)
	if err != nil {
		return nil, errors.Trace(err)
	}
	publicParam := gwacl.NewResourceExtensionParameter("CustomScriptExtensionPublicConfigParameter", scriptPublicConfig, gwacl.ResourceExtensionParameterTypePublic)
	return gwacl.NewResourceExtensionReference("MyCustomScriptExtension", "Microsoft.Compute", "CustomScriptExtension", "1.4", "", []gwacl.ResourceExtensionParameter{*publicParam}), nil
}

func makeUserdataResourceScripts(uri, filename string) (publicParam string, err error) {
	type publicConfig struct {
		FileUris         []string `json:"fileUris"`
		CommandToExecute string   `json:"commandToExecute"`
	}

	public := publicConfig{
		FileUris:         []string{uri},
		CommandToExecute: fmt.Sprintf("powershell -ExecutionPolicy Unrestricted -file %s", filename),
	}

	publicConf, err := json.Marshal(public)
	if err != nil {
		return "", errors.Trace(err)
	}

	scriptPublicConfig := base64.StdEncoding.EncodeToString(publicConf)

	return scriptPublicConfig, nil
}
*/

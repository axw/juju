package model

const (
	AttrAPTFTPProxy              = "apt-ftp-proxy"
	AttrAPTHTTPProxy             = "apt-http-proxy"
	AttrAPTHTTPSProxy            = "apt-https-proxy"
	AttrAPTMirror                = "apt-mirror"
	AttrAuthorizedKeys           = "authorized-keys"
	AttrAutoRetryHooks           = "automatically-retry-hooks"
	AttrBlockStorageDefault      = "default-block-storage"
	AttrDisableNetworkManagement = "disable-network-management"
	AttrFTPProxy                 = "ftp-proxy"
	AttrFirewallMode             = "firewall-mode"
	AttrHTTPProxy                = "http-proxy"
	AttrHTTPSProxy               = "https-proxy"
	AttrIgnoreMachineAddresses   = "ignore-machine-addresses"
	AttrImageMetadataURL         = "image-metadata-url"
	AttrImageStream              = "image-stream"
	AttrLoggingConfig            = "logging-config"
	AttrNoProxy                  = "no-proxy"
	AttrOSRefreshUpdate          = "enable-os-refresh-update"
	AttrOSUpgrade                = "enable-os-upgrade"
	AttrProvisionerHarvestMode   = "provisioner-harvest-mode"
	AttrResourceTags             = "resource-tags"
	AttrSSLHostnameValidation    = "ssl-hostname-validation"

	// TODO(axw) remove?
	AttrAgentMetadataURL = "agent-metadata-url"
	AttrAgentStream      = "agent-stream"
	AttrDevelopment      = "development"
	AttrTestMode         = "test-mode"
)

type Config struct {
	attrs map[string]interface{}
}

func NewConfig(attrs map[string]interface{}) (Config, error) {
	// TODO(axw) validate
	return Config{attrs}, nil
}

// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package imageutils

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/juju/errors"

	"github.com/juju/juju/environs/instances"
)

const (
	ubuntuImageIdPrefix  = `[[:xdigit:]]+__`
	ubuntuImageIdName    = `Ubuntu(?:_DAILY_BUILD)?`
	ubuntuImageIdRelease = `\d+_\d+(?:_\d+)?(?:-LTS)?`
	ubuntuImageIdVersion = `\d+`
)

var (
	ubuntuImageIdRegex = regexp.MustCompile(fmt.Sprintf(
		"^%s(%s)-(%s)-amd64-server-(%s)-.*$",
		ubuntuImageIdPrefix,
		ubuntuImageIdName,
		ubuntuImageIdRelease,
		ubuntuImageIdVersion,
	))

	azureUrnIdRegex = regexp.MustCompile(
		`^([^:]+):([^:]+):([^:]+):([^:]+)$`,
	)
)

// InstanceSpecImageReference returns a compute.ImageReference given a
// instances.Image. We attempt to parse the image ID in the form such as:
//    "b39f27a8b8c64d52b05eac6a62ebad85__Ubuntu-15_04-amd64-server-20151015-en-us-30GB"
// and, if that does not match, then attempt to parse the ID in the URN form
// such as:
//    "Canonical:UbuntuServer:14.04.3-DAILY-LTS:14.04.201509280"
//
// TODO(axw) find out about getting information added to simplestreams
// so that we don't have to do this.
func ImageReference(image instances.Image) (*compute.ImageReference, error) {
	submatch := ubuntuImageIdRegex.FindStringSubmatch(image.Id)
	if submatch != nil {
		return ubuntuImageReference(submatch), nil
	}
	submatch = azureUrnIdRegex.FindStringSubmatch(image.Id)
	if submatch != nil {
		return &compute.ImageReference{
			Publisher: to.StringPtr(submatch[1]),
			Offer:     to.StringPtr(submatch[2]),
			Sku:       to.StringPtr(submatch[3]),
			Version:   to.StringPtr(submatch[4]),
		}, nil
	}
	return nil, errors.Errorf("failed to extract URN from image ID %q", image.Id)
}

func ubuntuImageReference(submatch []string) *compute.ImageReference {
	name := submatch[1]
	sku := strings.Replace(submatch[2], "_", ".", 2)
	if name == "Ubuntu_DAILY_BUILD" {
		if strings.HasSuffix(sku, "-LTS") {
			sku = sku[:len(sku)-len("-LTS")]
			sku = sku + "-DAILY-LTS"
		} else {
			sku = sku + "-DAILY"
		}
	}

	// 14.04.3-LTS published at 201509280 => 14.04.201509280
	publishDate := submatch[3]
	version := sku
	if i := strings.IndexRune(version, '-'); i > 0 {
		version = version[:i]
	}
	version = strings.Join(strings.Split(version, ".")[:2], ".")
	version = fmt.Sprintf("%s.%s", version, publishDate)

	return &compute.ImageReference{
		Publisher: to.StringPtr("Canonical"),
		Offer:     to.StringPtr("UbuntuServer"),
		Sku:       to.StringPtr(sku),
		Version:   to.StringPtr(version),
	}
}

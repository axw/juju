// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/commerce"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/juju/errors"
	"github.com/juju/utils"
)

var (
	nowish = time.Now()
)

func Main() (int, error) {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [-o outfile] subscription-id:\n", os.Args[0])
	}

	var outfilename string
	flag.StringVar(&outfilename, "o", "-", "Name of a file to write the output to")
	flag.Parse()

	var subscriptionId string
	switch n := flag.NArg(); n {
	case 1:
		subscriptionId = flag.Args()[0]
	default:
		fmt.Println(flag.Args())
		flag.Usage()
		return 2, nil
	}

	fout := os.Stdout
	if outfilename != "-" {
		var err error
		fout, err = os.Create(outfilename)
		if err != nil {
			return -1, err
		}
		defer fout.Close()
	}

	tmpl := template.Must(template.New("instanceTypes").Parse(`
// Copyright {{.Year}} Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azureinstancetypes

import (
	"github.com/juju/utils/arch"

	"github.com/juju/juju/environs/instances"
)

var (
	hyperv = "Hyper-V"
	amd64  = []string{arch.AMD64}
)

var allInstanceTypes = map[string][]instances.InstanceType{
{{range $region, $instanceTypes := .InstanceTypes}}
{{printf "%q: {" $region}}
{{range $index, $instanceType := $instanceTypes}}{{with $instanceType}}
  {
    Name:       {{printf "%q" .Name}},
    Arches:     amd64,
    CpuCores:   {{.CpuCores}},
    Mem:        {{.Mem}},
    RootDisk:   {{.RootDisk}},
    VirtType:   &hyperv,
    Cost:       {{.Cost}},
    {{if .Deprecated}}Deprecated: true,{{end}}
  },
{{end}}{{end}}
},
{{end}}
}`))

	authorizer, err := getAuthorizer(subscriptionId)
	if err != nil {
		return -1, err
	}

	instanceTypes, err := process(subscriptionId, authorizer)
	if err != nil {
		return -1, err
	}

	templateData := struct {
		Year          int
		InstanceTypes map[string][]instanceType
	}{
		nowish.Year(),
		instanceTypes,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return -1, err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return -1, err
	}
	if _, err := fout.Write(formatted); err != nil {
		return -1, err
	}

	return 0, nil
}

func getAuthorizer(subscriptionId string) (autorest.Authorizer, error) {
	// TODO(axw) detect tenantId from subscriptionId
	const tenantId = "56b9e7ec-e96e-4252-af44-85be7be497ff"

	currentUser, err := user.Current()
	if err != nil {
		return nil, errors.Trace(err)
	}
	jsonFile := filepath.Join(currentUser.HomeDir, ".azure", "accessTokens.json")
	f, err := os.Open(jsonFile)
	if err != nil {
		return nil, errors.Trace(err)
	}
	defer f.Close()

	type token struct {
		Authority        string `json:"_authority"`
		ClientID         string `json:"_clientId"`
		UserID           string `json:"userId"`
		IsMRRT           bool   `json:"isMRRT"`
		IdentityProvider string `json:"identityProvider"`
		ExpiresIn        int    `json:"expiresIn"`
		ExpiresOn        string `json:"expiresOn"`
		AccessToken      string `json:"accessToken"`
		RefreshToken     string `json:"refreshToken"`
		Resource         string `json:"resource"`
		Type             string `json:"tokenType"`
	}

	var tokens []token
	if err := json.NewDecoder(f).Decode(&tokens); err != nil {
		return nil, errors.Trace(err)
	}
	for _, token := range tokens {
		if !strings.HasSuffix(token.Authority, tenantId) {
			continue
		}
		adalToken := adal.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
			ExpiresIn:    strconv.Itoa(token.ExpiresIn),
			ExpiresOn:    token.ExpiresOn,
			Resource:     token.Resource,
			Type:         token.Type,
		}
		return autorest.NewBearerAuthorizer(&adalToken), nil
	}
	return nil, errors.NotFoundf("token")
}

var subCategoryRe = regexp.MustCompile("^.* VM$")

func process(subscriptionId string, authorizer autorest.Authorizer) (map[string][]instanceType, error) {
	// Get the VM size information for one of the regions.
	// Use the "US West 2" location, which we expect to
	// always have the most up-to-date VM sizes.
	vmSizesClient := compute.NewVirtualMachineSizesClient(subscriptionId)
	vmSizesClient.Authorizer = authorizer

	rateCardClient := commerce.NewRateCardClient(subscriptionId)
	rateCardClient.Authorizer = authorizer
	filter := strings.Join([]string{
		// https://azure.microsoft.com/en-us/support/legal/offer-details/
		"OfferDurableId eq 'MS-AZR-0003P'",
		"Currency eq 'USD'",
		"Locale eq 'en-US'",
		"RegionInfo eq 'US'",
	}, " and ")
	info, err := rateCardClient.Get(filter)
	if err != nil {
		return nil, errors.Annotate(err, "querying rate card service")
	}

	instanceTypes := make(map[string][]instanceType)
	for _, meter := range *info.Meters {
		if to.String(meter.MeterName) != "Compute Hours" {
			continue
		}
		if to.String(meter.MeterCategory) != "Virtual Machines" {
			continue
		}
		subcat := to.String(meter.MeterSubCategory)
		if !subCategoryRe.MatchString(subcat) {
			log.Printf("ignoring meter info for subcategory %q", subcat)
			continue
		}
		vmSize := strings.Fields(subcat)[0]

		switch unit := to.String(meter.Unit); unit {
		case "Hours", "1 Hour":
		default:
			return nil, errors.Errorf("unexpected unit: %q", *meter.Unit)
		}

		meterRegion := to.String(meter.MeterRegion)
		if meterRegion == "" {
			continue
		}
		region, ok := meterRegions[meterRegion]
		if !ok {
			return nil, errors.Errorf("unexpected meter region %q", meterRegion)
		} else if region == "" {
			// Explicitly ignored region.
			continue
		}

		vmSize = strings.Replace(vmSize, "BASIC.", "Basic_", 1)
		if !strings.HasPrefix(vmSize, "Basic") && !strings.HasPrefix(vmSize, "Standard") {
			vmSize = "Standard_" + vmSize
		}

		// TODO(axw) include RootDisk info
		instanceType := instanceType{
			Name:       vmSize,
			Cost:       uint64(to.Float64((*meter.MeterRates)["0"]) * 1000),
			Deprecated: isDeprecated(vmSize),
		}
		instanceTypes[region] = append(instanceTypes[region], instanceType)
	}

	for location, instanceTypes := range instanceTypes {
		log.Println("fetching VM sizes for", location)
		sizesResult, err := vmSizesClient.List(location)
		if err != nil {
			return nil, errors.Annotate(err, "listing VM sizes")
		}
		vmSizes := make(map[string]compute.VirtualMachineSize)
		for _, value := range *sizesResult.Value {
			vmSizes[to.String(value.Name)] = value
		}
		for i, instanceType := range instanceTypes {
			size, ok := vmSizes[instanceType.Name]
			if !ok {
				return nil, errors.Errorf("unexpected VM size %q", instanceType.Name)
			}
			instanceType.CpuCores = uint64(to.Int32(size.NumberOfCores))
			instanceType.Mem = uint64(to.Int32(size.MemoryInMB))
			// NOTE(axw) size.OsDiskSizeInMB is the *maximum*
			// OS-disk size. When we create a VM, we can create
			// one that is smaller.
			instanceType.RootDisk = mbToMib(uint64(to.Int32(size.OsDiskSizeInMB)))
			instanceTypes[i] = instanceType
		}
	}

	// Sort the instance types by cost and then name.
	for _, instanceTypes := range instanceTypes {
		sort.Sort(byCostThenName(instanceTypes))
	}

	return instanceTypes, nil
}

var meterRegions = map[string]string{
	"AP East":          "eastasia",
	"AP Southeast":     "southeastasia",
	"AU East":          "australiaeast",
	"AU Southeast":     "australiasoutheast",
	"BR South":         "brazilsouth",
	"CA East":          "canadaeast",
	"CA Central":       "canadacentral",
	"EU North":         "northeurope",
	"EU West":          "westeurope",
	"IN West":          "westindia",
	"IN South":         "southindia",
	"IN Central":       "centralindia",
	"JA East":          "japaneast",
	"JA West":          "japanwest",
	"KR South":         "koreasouth",
	"KR Central":       "koreacentral",
	"UK West":          "ukwest",
	"UK South":         "uksouth",
	"US East":          "eastus",
	"US East 2":        "eastus2",
	"US North Central": "northcentralus",
	"US South Central": "southcentralus",
	"US West Central":  "westcentralus",
	"US Central":       "centralus",
	"US West":          "westus",
	"US West 2":        "westus2",

	// Regions we don't care about.
	"USGov":     "",
	"US Gov TX": "",
	"US Gov AZ": "",
}

func parseMem(s string) (uint64, error) {
	s = strings.Replace(s, " ", "", -1)
	s = strings.Replace(s, ",", "", -1) // e.g. 1,952 -> 1952

	// Sometimes it's GiB, sometimes Gib. We don't like Gib.
	s = strings.Replace(s, "Gib", "GiB", 1)
	return utils.ParseSize(s)
}

func isDeprecated(vmSize string) bool {
	if strings.HasPrefix(vmSize, "Basic_") {
		// We mark basic instance types as deprecated,
		// so that they are not automatically chosen.
		// Basic VM sizes lack features like availability
		// sets, which makes them unsuitable for some
		// workloads. They should only be used if the
		// user explicitly requests them.
		return true
	}
	return false
}

type instanceType struct {
	Name       string
	CpuCores   uint64
	Mem        uint64
	Cost       uint64
	RootDisk   uint64
	Deprecated bool // i.e. not current generation
}

func mbToMib(mb uint64) uint64 {
	b := mb * 1000 * 1000
	return uint64(float64(b) / 1024 / 1024)
}

type byCostThenName []instanceType

func (b byCostThenName) Len() int {
	return len(b)
}

func (b byCostThenName) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byCostThenName) Less(i, j int) bool {
	ti := b[i]
	tj := b[j]
	if ti.Cost < tj.Cost {
		return true
	}
	if tj.Cost < ti.Cost {
		return false
	}
	return ti.Name < tj.Name
}

func main() {
	rc, err := Main()
	if err != nil {
		log.Fatal(err)
	}
	os.Exit(rc)
}

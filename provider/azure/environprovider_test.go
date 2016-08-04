// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/provider/azure"
	"github.com/juju/juju/provider/azure/internal/azuretesting"
	"github.com/juju/juju/testing"
)

type environProviderSuite struct {
	testing.BaseSuite
	provider environs.EnvironProvider
	requests []*http.Request
	sender   azuretesting.Senders
}

var _ = gc.Suite(&environProviderSuite{})

func (s *environProviderSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.provider = newProvider(c, azure.ProviderConfig{
		Sender:           &s.sender,
		RequestInspector: requestRecorder(&s.requests),
	})
	s.sender = nil
}

func fakeUserPassCredential() *cloud.Credential {
	cred := cloud.NewCredential(
		cloud.UserPassAuthType,
		map[string]string{
			"application-id":       fakeApplicationId,
			"subscription-id":      fakeSubscriptionId,
			"tenant-id":            fakeTenantId,
			"application-password": "opensezme",
		},
	)
	return &cred
}

func (s *environProviderSuite) TestPrepareConfig(c *gc.C) {
	cfg := makeTestModelConfig(c)
	s.sender = azuretesting.Senders{tokenRefreshSender()}
	cfg, err := s.provider.PrepareConfig(environs.PrepareConfigParams{
		Config: cfg,
		Cloud: environs.CloudSpec{
			Type:            "azure",
			Name:            "azure",
			Region:          "westus",
			Endpoint:        "https://api.azurestack.local",
			StorageEndpoint: "https://storage.azurestack.local",
			Credential:      fakeUserPassCredential(),
		},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Check(cfg, gc.NotNil)
}

func newProvider(c *gc.C, config azure.ProviderConfig) environs.EnvironProvider {
	if config.NewStorageClient == nil {
		var storage azuretesting.MockStorageClient
		config.NewStorageClient = storage.NewClient
	}
	if config.StorageAccountNameGenerator == nil {
		config.StorageAccountNameGenerator = func() string {
			return fakeStorageAccount
		}
	}
	if config.RetryClock == nil {
		config.RetryClock = testing.NewClock(time.Time{})
	}
	environProvider, err := azure.NewProvider(config)
	c.Assert(err, jc.ErrorIsNil)
	return environProvider
}

func requestRecorder(requests *[]*http.Request) autorest.PrepareDecorator {
	if requests == nil {
		return nil
	}
	var mu sync.Mutex
	return func(p autorest.Preparer) autorest.Preparer {
		return autorest.PreparerFunc(func(req *http.Request) (*http.Request, error) {
			// Save the request body, since it will be consumed.
			reqCopy := *req
			if req.Body != nil {
				var buf bytes.Buffer
				if _, err := buf.ReadFrom(req.Body); err != nil {
					return nil, err
				}
				if err := req.Body.Close(); err != nil {
					return nil, err
				}
				reqCopy.Body = ioutil.NopCloser(&buf)
				req.Body = ioutil.NopCloser(bytes.NewReader(buf.Bytes()))
			}
			mu.Lock()
			*requests = append(*requests, &reqCopy)
			mu.Unlock()
			return req, nil
		})
	}
}

// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs"
	envtesting "github.com/juju/juju/environs/testing"
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
	s.provider = newEnvironProvider(c, &s.sender, &s.requests)
	s.sender = nil
}

func (s *environProviderSuite) TestPrepareForBootstrapWithInternalConfig(c *gc.C) {
	s.testPrepareForBootstrapWithInternalConfig(c, "controller-uuid")
	s.testPrepareForBootstrapWithInternalConfig(c, "storage-account")
}

func (s *environProviderSuite) testPrepareForBootstrapWithInternalConfig(c *gc.C, key string) {
	ctx := envtesting.BootstrapContext(c)
	cfg := makeTestEnvironConfig(c, testing.Attrs{key: "whatever"})
	s.sender = azuretesting.Senders{tokenRefreshSender()}
	_, err := s.provider.PrepareForBootstrap(ctx, cfg)
	c.Check(err, gc.ErrorMatches, fmt.Sprintf(`internal config "%s" must not be specified`, key))
}

func (s *environProviderSuite) TestPrepareForBootstrap(c *gc.C) {
	ctx := envtesting.BootstrapContext(c)
	cfg := makeTestEnvironConfig(c)
	cfg, err := cfg.Remove([]string{"controller-uuid"})
	c.Assert(err, jc.ErrorIsNil)

	s.sender = azuretesting.Senders{tokenRefreshSender()}
	env, err := s.provider.PrepareForBootstrap(ctx, cfg)
	c.Check(err, jc.ErrorIsNil)
	c.Check(env, gc.NotNil)
}

func newEnvironProvider(c *gc.C, sender autorest.Sender, requests *[]*http.Request) environs.EnvironProvider {
	var requestInspector autorest.PrepareDecorator
	if requests != nil {
		requestInspector = requestRecorder(requests)
	}
	config := azure.EnvironProviderConfig{
		Sender:           sender,
		RequestInspector: requestInspector,
	}
	provider, err := azure.NewEnvironProvider(config)
	c.Assert(err, jc.ErrorIsNil)
	return provider
}

func requestRecorder(requests *[]*http.Request) autorest.PrepareDecorator {
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
			*requests = append(*requests, &reqCopy)
			return req, nil
		})
	}
}

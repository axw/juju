// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"bytes"
	"io/ioutil"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/provider/azure"
)

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

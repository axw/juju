// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"net/http"
	"net/http/httputil"

	"github.com/Azure/go-autorest/autorest"
	"github.com/juju/loggo"
)

// azureRequestTracer is an Azure/autorest request/response
// decorator that logs requests and responses at trace level.
type azureRequestTracer struct {
	logger loggo.Logger
}

func (t azureRequestTracer) WithInspection() autorest.PrepareDecorator {
	return func(p autorest.Preparer) autorest.Preparer {
		return autorest.PreparerFunc(func(r *http.Request) (*http.Request, error) {
			dump, err := httputil.DumpRequest(r, true)
			if err != nil {
				t.logger.Tracef("failed to dump request: %v", err)
				t.logger.Tracef("%+v", r)
			} else {
				t.logger.Tracef("%s", dump)
			}
			return p.Prepare(r)
		})
	}
}

func (t azureRequestTracer) ByInspecting() autorest.RespondDecorator {
	return func(r autorest.Responder) autorest.Responder {
		return autorest.ResponderFunc(func(resp *http.Response) error {
			dump, err := httputil.DumpResponse(resp, true)
			if err != nil {
				t.logger.Tracef("failed to dump response: %v", err)
				t.logger.Tracef("%+v", resp)
			} else {
				t.logger.Tracef("%s", dump)
			}
			return r.Respond(resp)
		})
	}
}

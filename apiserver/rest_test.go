// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/core/crossmodel"
	"github.com/juju/juju/state"
	"github.com/juju/juju/testcharms"
	jujuFactory "github.com/juju/juju/testing/factory"
)

type restCommonSuite struct {
	apiserverBaseSuite
}

func (s *restCommonSuite) restURL(path string) *url.URL {
	return s.URL(fmt.Sprintf("/model/%s/rest/1.0/%s", s.State.ModelUUID(), path), nil)
}

func (s *restCommonSuite) restURI(c *gc.C, path string) string {
	return s.restURL(path).String()
}

func (s *restCommonSuite) assertGetFileResponse(c *gc.C, resp *http.Response, expBody, expContentType string) {
	body := assertResponse(c, resp, http.StatusOK, expContentType)
	c.Check(string(body), gc.Equals, expBody)
}

func (s *restCommonSuite) assertErrorResponse(c *gc.C, resp *http.Response, expCode int, expError string) {
	charmResponse := s.assertResponse(c, resp, expCode)
	c.Check(charmResponse.Error, gc.Matches, expError)
}

func (s *restCommonSuite) assertResponse(c *gc.C, resp *http.Response, expStatus int) params.CharmsResponse {
	body := assertResponse(c, resp, expStatus, params.ContentTypeJSON)
	var charmResponse params.CharmsResponse
	err := json.Unmarshal(body, &charmResponse)
	c.Assert(err, jc.ErrorIsNil, gc.Commentf("body: %s", body))
	return charmResponse
}

type restSuite struct {
	restCommonSuite
}

var _ = gc.Suite(&restSuite{})

func (s *restSuite) SetUpSuite(c *gc.C) {
	if runtime.GOOS != "linux" {
		c.Skip("apiservers only run on linux")
	}
	s.restCommonSuite.SetUpSuite(c)
}

func (s *restSuite) TestRestServedSecurely(c *gc.C) {
	url := s.restURL("")
	url.Scheme = "http"
	sendHTTPRequest(c, httpRequestParams{
		method:      "GET",
		url:         url.String(),
		expectError: `.*malformed HTTP response.*`,
	})
}

func (s *restSuite) TestGETRequiresAuth(c *gc.C) {
	resp := sendHTTPRequest(c, httpRequestParams{method: "GET", url: s.restURI(c, "entity/name/attribute")})
	body := assertResponse(c, resp, http.StatusUnauthorized, "text/plain; charset=utf-8")
	c.Assert(string(body), gc.Equals, "authentication failed: no credentials provided\n")
}

func (s *restSuite) TestRequiresGET(c *gc.C) {
	resp := s.sendHTTPRequest(c, httpRequestParams{method: "POST", url: s.restURI(c, "entity/name/attribute")})
	s.assertErrorResponse(c, resp, http.StatusMethodNotAllowed, `unsupported method: "POST"`)
}

func (s *restSuite) TestGetReturnsNotFoundWhenMissing(c *gc.C) {
	uri := s.restURI(c, "remote-application/foo/attribute")
	resp := s.sendHTTPRequest(c, httpRequestParams{method: "GET", url: uri})
	s.assertErrorResponse(
		c, resp, http.StatusNotFound,
		`cannot retrieve model data: remote application "foo" not found`,
	)
}

func (s *restSuite) charmsURI(query string) string {
	url := s.URL(fmt.Sprintf("/model/%s/charms", s.State.ModelUUID()), nil)
	url.RawQuery = query
	return url.String()
}

// TODO fix this test
func (s *restSuite) TestGetRemoteApplicationIcon(c *gc.C) {
	s.Factory.MakeApplication(c, nil)

	// Add an offer for the application.
	offers := state.NewApplicationOffers(s.State)
	offer, err := offers.AddOffer(crossmodel.AddApplicationOfferArgs{
		OfferName:       "remote-app-offer",
		ApplicationName: "mysql",
		Owner:           s.Owner.Id(),
	})
	c.Assert(err, jc.ErrorIsNil)
	// Set up a charm entry for dummy app with no charm in storage.
	factory := jujuFactory.NewFactory(s.State)
	dummyCh := factory.MakeCharm(c, &jujuFactory.CharmParams{
		Name: "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = s.State.AddApplication(state.AddApplicationArgs{
		Name:  "dummy",
		Charm: dummyCh,
	})
	c.Assert(err, jc.ErrorIsNil)
	offer2, err := offers.AddOffer(crossmodel.AddApplicationOfferArgs{
		OfferName:       "notfound-remote-app-offer",
		ApplicationName: "dummy",
		Owner:           s.Owner.Id(),
	})
	c.Assert(err, jc.ErrorIsNil)

	// Add remote applications to other model which we will query below.
	otherModelState := s.Factory.MakeModel(c, nil)
	defer otherModelState.Close()
	_, err = otherModelState.AddRemoteApplication(state.AddRemoteApplicationParams{
		Name:        "remote-app",
		SourceModel: s.IAASModel.ModelTag(),
		OfferUUID:   offer.OfferUUID,
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = otherModelState.AddRemoteApplication(state.AddRemoteApplicationParams{
		Name:        "notfound-remote-app",
		SourceModel: s.IAASModel.ModelTag(),
		OfferUUID:   offer2.OfferUUID,
	})
	c.Assert(err, jc.ErrorIsNil)

	// Prepare the tests.
	svgMimeType := mime.TypeByExtension(".svg")
	iconPath := filepath.Join(testcharms.Repo.CharmDirPath("mysql"), "icon.svg")
	icon, err := ioutil.ReadFile(iconPath)
	c.Assert(err, jc.ErrorIsNil)
	tests := []struct {
		about      string
		query      string
		expectType string
		expectBody string
	}{{
		about:      "icon found",
		query:      "remote-application/remote-app/icon",
		expectBody: string(icon),
	}, {
		about:      "icon not found",
		query:      "remote-application/notfound-remote-app/icon",
		expectBody: common.DefaultCharmIcon,
	}}

	for i, test := range tests {
		c.Logf("\ntest %d: %s", i, test.about)
		uri := s.restURI(c, test.query)
		resp := s.sendHTTPRequest(c, httpRequestParams{method: "GET", url: uri})
		if test.expectType == "" {
			test.expectType = svgMimeType
		}
		s.assertGetFileResponse(c, resp, test.expectBody, test.expectType)
	}
}

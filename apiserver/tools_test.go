// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	"github.com/juju/utils/arch"
	"github.com/juju/utils/series"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/params"
	envtools "github.com/juju/juju/environs/tools"
	toolstesting "github.com/juju/juju/environs/tools/testing"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/binarystorage"
	"github.com/juju/juju/testing/factory"
	coretools "github.com/juju/juju/tools"
	jujuversion "github.com/juju/juju/version"
)

type toolsSuite struct {
	apiserverBaseSuite
}

var _ = gc.Suite(&toolsSuite{})

func (s *toolsSuite) toolsURL(query string) *url.URL {
	return s.modelToolsURL(s.Model.UUID(), query)
}

func (s *toolsSuite) modelToolsURL(model, query string) *url.URL {
	u := s.URL(fmt.Sprintf("/model/%s/tools", model), nil)
	u.RawQuery = query
	return u
}

func (s *toolsSuite) toolsURI(query string) string {
	if query != "" && query[0] == '?' {
		query = query[1:]
	}
	return s.toolsURL(query).String()
}

func (s *toolsSuite) uploadRequest(c *gc.C, url, contentType string, content io.Reader) *http.Response {
	return s.sendHTTPRequest(c, httpRequestParams{
		method:      "POST",
		url:         url,
		contentType: contentType,
		body:        content,
	})
}

func (s *toolsSuite) downloadRequest(c *gc.C, version version.Binary, uuid string) *http.Response {
	url := s.toolsURL("")
	if uuid == "" {
		url.Path = fmt.Sprintf("/tools/%s", version)
	} else {
		url.Path = fmt.Sprintf("/model/%s/tools/%s", uuid, version)
	}
	return sendHTTPRequest(c, httpRequestParams{method: "GET", url: url.String()})
}

func (s *toolsSuite) assertUploadResponse(c *gc.C, resp *http.Response, agentTools *coretools.Tools) {
	toolsResponse := s.assertResponse(c, resp, http.StatusOK)
	c.Check(toolsResponse.Error, gc.IsNil)
	c.Check(toolsResponse.ToolsList, jc.DeepEquals, coretools.List{agentTools})
}

func (s *toolsSuite) assertJSONErrorResponse(c *gc.C, resp *http.Response, expCode int, expError string) {
	toolsResponse := s.assertResponse(c, resp, expCode)
	c.Check(toolsResponse.ToolsList, gc.IsNil)
	c.Check(toolsResponse.Error, gc.NotNil)
	c.Check(toolsResponse.Error.Message, gc.Matches, expError)
}

func (s *toolsSuite) assertPlainErrorResponse(c *gc.C, resp *http.Response, expCode int, expError string) {
	body := assertResponse(c, resp, expCode, "text/plain; charset=utf-8")
	c.Assert(string(body), gc.Matches, expError+"\n")
}

func (s *toolsSuite) assertResponse(c *gc.C, resp *http.Response, expStatus int) params.ToolsResult {
	body := assertResponse(c, resp, expStatus, params.ContentTypeJSON)
	var toolsResponse params.ToolsResult
	err := json.Unmarshal(body, &toolsResponse)
	c.Assert(err, jc.ErrorIsNil, gc.Commentf("body: %s", body))
	return toolsResponse
}

func (s *toolsSuite) TestToolsUploadedSecurely(c *gc.C) {
	url := s.toolsURL("")
	url.Scheme = "http"
	sendHTTPRequest(c, httpRequestParams{
		method:      "PUT",
		url:         url.String(),
		expectError: `.*malformed HTTP response.*`,
	})
}

func (s *toolsSuite) TestRequiresAuth(c *gc.C) {
	resp := sendHTTPRequest(c, httpRequestParams{method: "GET", url: s.toolsURI("")})
	s.assertPlainErrorResponse(c, resp, http.StatusUnauthorized, "authentication failed: no credentials provided")
}

func (s *toolsSuite) TestRequiresPOST(c *gc.C) {
	resp := s.sendHTTPRequest(c, httpRequestParams{method: "PUT", url: s.toolsURI("")})
	s.assertJSONErrorResponse(c, resp, http.StatusMethodNotAllowed, `unsupported method: "PUT"`)
}

func (s *toolsSuite) TestAuthRequiresUser(c *gc.C) {
	// Add a machine and try to login.
	machine, err := s.State.AddMachine("quantal", state.JobHostUnits)
	c.Assert(err, jc.ErrorIsNil)
	err = machine.SetProvisioned("foo", "fake_nonce", nil)
	c.Assert(err, jc.ErrorIsNil)
	password, err := utils.RandomPassword()
	c.Assert(err, jc.ErrorIsNil)
	err = machine.SetPassword(password)
	c.Assert(err, jc.ErrorIsNil)

	resp := sendHTTPRequest(c, httpRequestParams{
		tag:      machine.Tag().String(),
		password: password,
		method:   "POST",
		url:      s.toolsURI(""),
		nonce:    "fake_nonce",
	})
	s.assertPlainErrorResponse(
		c, resp, http.StatusForbidden,
		"authorization failed: tag kind machine not valid",
	)

	// Now try a user login.
	resp = s.sendHTTPRequest(c, httpRequestParams{method: "POST", url: s.toolsURI("")})
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "expected binaryVersion argument")
}

func (s *toolsSuite) TestUploadRequiresVersion(c *gc.C) {
	resp := s.sendHTTPRequest(c, httpRequestParams{method: "POST", url: s.toolsURI("")})
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "expected binaryVersion argument")
}

func (s *toolsSuite) TestUploadFailsWithNoTools(c *gc.C) {
	var empty bytes.Buffer
	resp := s.uploadRequest(c, s.toolsURI("?binaryVersion=1.18.0-quantal-amd64"), "application/x-tar-gz", &empty)
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "no agent binaries uploaded")
}

func (s *toolsSuite) TestUploadFailsWithInvalidContentType(c *gc.C) {
	var empty bytes.Buffer
	// Now try with the default Content-Type.
	resp := s.uploadRequest(c, s.toolsURI("?binaryVersion=1.18.0-quantal-amd64"), "application/octet-stream", &empty)
	s.assertJSONErrorResponse(
		c, resp, http.StatusBadRequest, "expected Content-Type: application/x-tar-gz, got: application/octet-stream")
}

func (s *toolsSuite) setupToolsForUpload(c *gc.C) (coretools.List, version.Binary, []byte) {
	localStorage := c.MkDir()
	vers := version.MustParseBinary("1.9.0-quantal-amd64")
	versionStrings := []string{vers.String()}
	expectedTools := toolstesting.MakeToolsWithCheckSum(c, localStorage, "released", versionStrings)
	toolsFile := envtools.StorageName(vers, "released")
	toolsContent, err := ioutil.ReadFile(filepath.Join(localStorage, toolsFile))
	c.Assert(err, jc.ErrorIsNil)
	return expectedTools, vers, toolsContent
}

func (s *toolsSuite) TestUpload(c *gc.C) {
	// Make some fake tools.
	expectedTools, v, toolsContent := s.setupToolsForUpload(c)
	vers := v.String()

	// Now try uploading them.
	resp := s.uploadRequest(
		c, s.toolsURI("?binaryVersion="+vers),
		"application/x-tar-gz",
		bytes.NewReader(toolsContent),
	)

	// Check the response.
	expectedTools[0].URL = s.toolsURL("").String() + "/" + vers
	s.assertUploadResponse(c, resp, expectedTools[0])

	// Check the contents.
	metadata, uploadedData := s.getToolsFromStorage(c, s.State, vers)
	c.Assert(uploadedData, gc.DeepEquals, toolsContent)
	allMetadata := s.getToolsMetadataFromStorage(c, s.State)
	c.Assert(allMetadata, jc.DeepEquals, []binarystorage.Metadata{metadata})
}

func (s *toolsSuite) TestMigrateTools(c *gc.C) {
	// Make some fake tools.
	expectedTools, v, toolsContent := s.setupToolsForUpload(c)
	vers := v.String()

	newSt := s.Factory.MakeModel(c, nil)
	defer newSt.Close()
	importedModel, err := newSt.Model()
	c.Assert(err, jc.ErrorIsNil)
	err = importedModel.SetMigrationMode(state.MigrationModeImporting)
	c.Assert(err, jc.ErrorIsNil)

	// Now try uploading them.
	uri := s.URL("/migrate/tools", url.Values{"binaryVersion": {vers}})
	resp := s.sendHTTPRequest(c, httpRequestParams{
		method:      "POST",
		url:         uri.String(),
		contentType: "application/x-tar-gz",
		body:        bytes.NewReader(toolsContent),
		extraHeaders: map[string]string{
			params.MigrationModelHTTPHeader: importedModel.UUID(),
		},
	})

	// Check the response.
	expectedTools[0].URL = s.modelToolsURL(s.State.ControllerModelUUID(), "").String() + "/" + vers
	s.assertUploadResponse(c, resp, expectedTools[0])

	// Check the contents.
	metadata, uploadedData := s.getToolsFromStorage(c, newSt, vers)
	c.Assert(uploadedData, gc.DeepEquals, toolsContent)
	allMetadata := s.getToolsMetadataFromStorage(c, newSt)
	c.Assert(allMetadata, jc.DeepEquals, []binarystorage.Metadata{metadata})
}

func (s *toolsSuite) TestMigrateToolsNotMigrating(c *gc.C) {
	// Make some fake tools.
	_, v, toolsContent := s.setupToolsForUpload(c)
	vers := v.String()

	newSt := s.Factory.MakeModel(c, nil)
	defer newSt.Close()

	uri := s.URL("/migrate/tools", url.Values{"binaryVersion": {vers}})
	resp := s.sendHTTPRequest(c, httpRequestParams{
		method:      "POST",
		url:         uri.String(),
		contentType: "application/x-tar-gz",
		body:        bytes.NewReader(toolsContent),
		extraHeaders: map[string]string{
			params.MigrationModelHTTPHeader: newSt.ModelUUID(),
		},
	})

	// Now try uploading them.
	s.assertJSONErrorResponse(
		c, resp, http.StatusBadRequest,
		`model migration mode is "" instead of "importing"`,
	)
}

func (s *toolsSuite) TestMigrateToolsUnauth(c *gc.C) {
	// Try uploading as a non controller admin.
	url := s.URL("/migrate/tools", nil).String()
	user := s.Factory.MakeUser(c, &factory.UserParams{Password: "hunter2"})
	resp := sendHTTPRequest(c, httpRequestParams{
		method:   "POST",
		url:      url,
		tag:      user.Tag().String(),
		password: "hunter2",
	})
	s.assertPlainErrorResponse(
		c, resp, http.StatusForbidden,
		"authorization failed: user .* is not a controller admin",
	)
}

func (s *toolsSuite) TestBlockUpload(c *gc.C) {
	// Make some fake tools.
	_, v, toolsContent := s.setupToolsForUpload(c)
	vers := v.String()

	// Block all changes.
	err := s.State.SwitchBlockOn(state.ChangeBlock, "TestUpload")
	c.Assert(err, jc.ErrorIsNil)

	// Now try uploading them.
	resp := s.uploadRequest(
		c, s.toolsURI("?binaryVersion="+vers),
		"application/x-tar-gz",
		bytes.NewReader(toolsContent),
	)
	toolsResponse := s.assertResponse(c, resp, http.StatusBadRequest)
	c.Assert(toolsResponse.Error, jc.Satisfies, params.IsCodeOperationBlocked)
	c.Assert(errors.Cause(toolsResponse.Error), gc.DeepEquals, &params.Error{
		Message: "TestUpload",
		Code:    "operation is blocked",
	})

	// Check the contents.
	storage, err := s.State.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	_, _, err = storage.Open(vers)
	c.Assert(errors.IsNotFound(err), jc.IsTrue)
}

func (s *toolsSuite) TestUploadAllowsTopLevelPath(c *gc.C) {
	// Backwards compatibility check, that we can upload tools to
	// https://host:port/tools
	expectedTools, vers, toolsContent := s.setupToolsForUpload(c)
	url := s.toolsURL("binaryVersion=" + vers.String())
	url.Path = "/tools"
	resp := s.uploadRequest(c, url.String(), "application/x-tar-gz", bytes.NewReader(toolsContent))
	expectedTools[0].URL = s.modelToolsURL(s.State.ControllerModelUUID(), "").String() + "/" + vers.String()
	s.assertUploadResponse(c, resp, expectedTools[0])
}

func (s *toolsSuite) TestUploadAllowsModelUUIDPath(c *gc.C) {
	// Check that we can upload tools to https://host:port/ModelUUID/tools
	expectedTools, vers, toolsContent := s.setupToolsForUpload(c)
	url := s.toolsURL("binaryVersion=" + vers.String())
	resp := s.uploadRequest(c, url.String(), "application/x-tar-gz", bytes.NewReader(toolsContent))
	// Check the response.
	expectedTools[0].URL = s.toolsURL("").String() + "/" + vers.String()
	s.assertUploadResponse(c, resp, expectedTools[0])
}

func (s *toolsSuite) TestUploadAllowsOtherModelUUIDPath(c *gc.C) {
	newSt := s.Factory.MakeModel(c, nil)
	defer newSt.Close()

	// Check that we can upload tools to https://host:port/ModelUUID/tools
	expectedTools, vers, toolsContent := s.setupToolsForUpload(c)
	url := s.modelToolsURL(newSt.ModelUUID(), "binaryVersion="+vers.String())
	resp := s.uploadRequest(c, url.String(), "application/x-tar-gz", bytes.NewReader(toolsContent))

	// Check the response.
	expectedTools[0].URL = s.modelToolsURL(newSt.ModelUUID(), "").String() + "/" + vers.String()
	s.assertUploadResponse(c, resp, expectedTools[0])
}

func (s *toolsSuite) TestUploadSeriesExpanded(c *gc.C) {
	// Make some fake tools.
	expectedTools, v, toolsContent := s.setupToolsForUpload(c)
	vers := v.String()
	// Now try uploading them. The tools will be cloned for
	// each additional series specified.
	params := "?binaryVersion=" + vers + "&series=quantal,precise"
	resp := s.uploadRequest(c, s.toolsURI(params), "application/x-tar-gz", bytes.NewReader(toolsContent))
	c.Assert(resp.StatusCode, gc.Equals, http.StatusOK)

	// Check the response.
	expectedTools[0].URL = s.toolsURL("").String() + "/" + vers
	s.assertUploadResponse(c, resp, expectedTools[0])

	// Check the contents.
	storage, err := s.State.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	for _, series := range []string{"precise", "quantal"} {
		v.Series = series
		_, r, err := storage.Open(v.String())
		c.Assert(err, jc.ErrorIsNil)
		uploadedData, err := ioutil.ReadAll(r)
		r.Close()
		c.Assert(err, jc.ErrorIsNil)
		c.Assert(uploadedData, gc.DeepEquals, toolsContent)
	}

	// ensure other series *aren't* there.
	v.Series = "trusty"
	_, err = storage.Metadata(v.String())
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}

func (s *toolsSuite) TestDownloadModelUUIDPath(c *gc.C) {
	v := version.Binary{
		Number: jujuversion.Current,
		Arch:   arch.HostArch(),
		Series: series.MustHostSeries(),
	}
	tools := s.storeFakeTools(c, s.State, "abc", binarystorage.Metadata{
		Version: v.String(),
		Size:    3,
		SHA256:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	})
	s.testDownload(c, tools, s.State.ModelUUID())
}

func (s *toolsSuite) TestDownloadOtherModelUUIDPath(c *gc.C) {
	newSt := s.Factory.MakeModel(c, nil)
	defer newSt.Close()

	v := version.Binary{
		Number: jujuversion.Current,
		Arch:   arch.HostArch(),
		Series: series.MustHostSeries(),
	}
	tools := s.storeFakeTools(c, newSt, "abc", binarystorage.Metadata{
		Version: v.String(),
		Size:    3,
		SHA256:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	})
	s.testDownload(c, tools, newSt.ModelUUID())
}

func (s *toolsSuite) TestDownloadTopLevelPath(c *gc.C) {
	v := version.Binary{
		Number: jujuversion.Current,
		Arch:   arch.HostArch(),
		Series: series.MustHostSeries(),
	}
	fmt.Println(s.State.ModelUUID())
	fmt.Println(s.State.ControllerModelUUID())
	tools := s.storeFakeTools(c, s.State, "abc", binarystorage.Metadata{
		Version: v.String(),
		Size:    3,
		SHA256:  "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	})
	s.testDownload(c, tools, "")
}

// TODO these three tests depend on JujuConnSuite. They should be moved
// to feature tests.

/*
func (s *toolsSuite) TestDownloadFetchesAndCaches(c *gc.C) {
	// The tools are not in binarystorage, so the download request causes
	// the API server to search for the tools in simplestreams, fetch
	// them, and then cache them in binarystorage.
	vers := version.MustParseBinary("1.23.0-trusty-amd64")
	stor := s.DefaultToolsStorage
	envtesting.RemoveTools(c, stor, "released")
	tools := envtesting.AssertUploadFakeToolsVersions(c, stor, "released", "released", vers)[0]
	data := s.testDownload(c, tools, "")

	metadata, cachedData := s.getToolsFromStorage(c, s.State, tools.Version.String())
	c.Assert(metadata.Size, gc.Equals, tools.Size)
	c.Assert(metadata.SHA256, gc.Equals, tools.SHA256)
	c.Assert(string(cachedData), gc.Equals, string(data))
}

func (s *toolsSuite) TestDownloadFetchesAndVerifiesSize(c *gc.C) {
	// Upload fake tools, then upload over the top so the SHA256 hash does not match.
	s.PatchValue(&jujuversion.Current, testing.FakeVersionNumber)
	stor := s.DefaultToolsStorage
	envtesting.RemoveTools(c, stor, "released")
	current := version.Binary{
		Number: jujuversion.Current,
		Arch:   arch.HostArch(),
		Series: series.MustHostSeries(),
	}
	tools := envtesting.AssertUploadFakeToolsVersions(c, stor, "released", "released", current)[0]
	err := stor.Put(envtools.StorageName(tools.Version, "released"), strings.NewReader("!"), 1)
	c.Assert(err, jc.ErrorIsNil)

	resp := s.downloadRequest(c, tools.Version, "")
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "error fetching agent binaries: size mismatch for .*")
	s.assertToolsNotStored(c, tools.Version.String())
}

func (s *toolsSuite) TestDownloadFetchesAndVerifiesHash(c *gc.C) {
	// Upload fake tools, then upload over the top so the SHA256 hash does not match.
	s.PatchValue(&jujuversion.Current, testing.FakeVersionNumber)
	stor := s.DefaultToolsStorage
	envtesting.RemoveTools(c, stor, "released")
	current := version.Binary{
		Number: jujuversion.Current,
		Arch:   arch.HostArch(),
		Series: series.MustHostSeries(),
	}
	tools := envtesting.AssertUploadFakeToolsVersions(c, stor, "released", "released", current)[0]
	sameSize := strings.Repeat("!", int(tools.Size))
	err := stor.Put(envtools.StorageName(tools.Version, "released"), strings.NewReader(sameSize), tools.Size)
	c.Assert(err, jc.ErrorIsNil)

	resp := s.downloadRequest(c, tools.Version, "")
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "error fetching agent binaries: hash mismatch for .*")
	s.assertToolsNotStored(c, tools.Version.String())
}
*/

func (s *toolsSuite) storeFakeTools(c *gc.C, st *state.State, content string, metadata binarystorage.Metadata) *coretools.Tools {
	storage, err := st.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	err = storage.Add(strings.NewReader(content), metadata)
	c.Assert(err, jc.ErrorIsNil)
	return &coretools.Tools{
		Version: version.MustParseBinary(metadata.Version),
		Size:    metadata.Size,
		SHA256:  metadata.SHA256,
	}
}

func (s *toolsSuite) getToolsFromStorage(c *gc.C, st *state.State, vers string) (binarystorage.Metadata, []byte) {
	storage, err := st.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	metadata, r, err := storage.Open(vers)
	c.Assert(err, jc.ErrorIsNil)
	data, err := ioutil.ReadAll(r)
	r.Close()
	c.Assert(err, jc.ErrorIsNil)
	return metadata, data
}

func (s *toolsSuite) getToolsMetadataFromStorage(c *gc.C, st *state.State) []binarystorage.Metadata {
	storage, err := st.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	metadata, err := storage.AllMetadata()
	c.Assert(err, jc.ErrorIsNil)
	return metadata
}

func (s *toolsSuite) assertToolsNotStored(c *gc.C, vers string) {
	storage, err := s.State.ToolsStorage()
	c.Assert(err, jc.ErrorIsNil)
	defer storage.Close()
	_, err = storage.Metadata(vers)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}

func (s *toolsSuite) testDownload(c *gc.C, tools *coretools.Tools, uuid string) []byte {
	resp := s.downloadRequest(c, tools.Version, uuid)
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(data, gc.HasLen, int(tools.Size))

	hash := sha256.New()
	hash.Write(data)
	c.Assert(fmt.Sprintf("%x", hash.Sum(nil)), gc.Equals, tools.SHA256)
	return data
}

// TODO the tests below should be removed entirely, or a subset (maybe one)
// moved to feature tests that are focused on macaroon authentication.
// The method of authentication is independent of any endpoint. It is enough
// to show that an endpoint is authenticated by testing basic auth.
// The *method* of authenti

/*
type toolsWithMacaroonsSuite struct {
	toolsSuite
}

var _ = gc.Suite(&toolsWithMacaroonsSuite{})

func (s *toolsWithMacaroonsSuite) SetUpTest(c *gc.C) {
	s.macaroonAuthEnabled = true
	s.toolsSuite.SetUpTest(c)
}

func (s *toolsWithMacaroonsSuite) TestWithNoBasicAuthReturnsDischargeRequiredError(c *gc.C) {
	resp := sendHTTPRequest(c, httpRequestParams{
		method: "POST",
		url:    s.toolsURI(""),
	})

	charmResponse := s.assertResponse(c, resp, http.StatusUnauthorized)
	c.Assert(charmResponse.Error, gc.NotNil)
	c.Assert(charmResponse.Error.Message, gc.Equals, "verification failed: no macaroons")
	c.Assert(charmResponse.Error.Code, gc.Equals, params.CodeDischargeRequired)
	c.Assert(charmResponse.Error.Info, gc.NotNil)
	c.Assert(charmResponse.Error.Info.Macaroon, gc.NotNil)
}

func (s *toolsWithMacaroonsSuite) TestCanPostWithDischargedMacaroon(c *gc.C) {
	checkCount := 0
	s.DischargerLogin = func() string {
		checkCount++
		return s.userTag.Id()
	}
	resp := sendHTTPRequest(c, httpRequestParams{
		do:     s.doer(),
		method: "POST",
		url:    s.toolsURI(""),
	})
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "expected binaryVersion argument")
	c.Assert(checkCount, gc.Equals, 1)
}

func (s *toolsWithMacaroonsSuite) TestCanPostWithLocalLogin(c *gc.C) {
	// Create a new local user that we can log in as
	// using macaroon authentication.
	const password = "hunter2"
	user := s.Factory.MakeUser(c, &factory.UserParams{Password: password})

	// Install a "web-page" visitor that deals with the interaction
	// method that Juju controllers support for authenticating local
	// users. Note: the use of httpbakery.NewMultiVisitor is necessary
	// to trigger httpbakery to query the authentication methods and
	// bypass browser authentication.
	var prompted bool
	jar := apitesting.NewClearableCookieJar()
	client := utils.GetNonValidatingHTTPClient()
	client.Jar = jar
	bakeryClient := httpbakery.NewClient()
	bakeryClient.Client = client
	bakeryClient.WebPageVisitor = httpbakery.NewMultiVisitor(apiauthentication.NewVisitor(
		user.UserTag().Id(),
		func(username string) (string, error) {
			c.Assert(username, gc.Equals, user.UserTag().Id())
			prompted = true
			return password, nil
		},
	))
	bakeryDo := func(req *http.Request) (*http.Response, error) {
		var body io.ReadSeeker
		if req.Body != nil {
			body = req.Body.(io.ReadSeeker)
			req.Body = nil
		}
		return bakeryClient.DoWithBodyAndCustomError(req, body, bakeryGetError)
	}

	resp := sendHTTPRequest(c, httpRequestParams{
		method:   "POST",
		url:      s.toolsURI(""),
		tag:      user.UserTag().String(),
		password: "", // no password forces macaroon usage
		do:       bakeryDo,
	})
	s.assertJSONErrorResponse(c, resp, http.StatusBadRequest, "expected binaryVersion argument")
	c.Assert(prompted, jc.IsTrue)
}

// doer returns a Do function that can make a bakery request
// appropriate for a charms endpoint.
func (s *toolsWithMacaroonsSuite) doer() func(*http.Request) (*http.Response, error) {
	return bakeryDo(nil, bakeryGetError)
}

// bakeryDo provides a function suitable for using in httpRequestParams.Do
// that will use the given http client (or utils.GetNonValidatingHTTPClient()
// if client is nil) and use the given getBakeryError function
// to translate errors in responses.
func bakeryDo(client *http.Client, getBakeryError func(*http.Response) error) func(*http.Request) (*http.Response, error) {
	bclient := httpbakery.NewClient()
	if client != nil {
		bclient.Client = client
	} else {
		// Configure the default client to skip verification/
		tlsConfig := utils.SecureTLSConfig()
		tlsConfig.InsecureSkipVerify = true
		bclient.Client.Transport = utils.NewHttpTLSTransport(tlsConfig)
	}
	return func(req *http.Request) (*http.Response, error) {
		var body io.ReadSeeker
		if req.Body != nil {
			body = req.Body.(io.ReadSeeker)
			req.Body = nil
		}
		return bclient.DoWithBodyAndCustomError(req, body, getBakeryError)
	}
}

// bakeryGetError implements a getError function
// appropriate for passing to httpbakery.Client.DoWithBodyAndCustomError
// for any endpoint that returns the error in a top level Error field.
func bakeryGetError(resp *http.Response) error {
	if resp.StatusCode != http.StatusUnauthorized {
		return nil
	}
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Annotatef(err, "cannot read body")
	}
	var errResp params.ErrorResult
	if err := json.Unmarshal(data, &errResp); err != nil {
		return errors.Annotatef(err, "cannot unmarshal body")
	}
	if errResp.Error == nil {
		return errors.New("no error found in error response body")
	}
	if errResp.Error.Code != params.CodeDischargeRequired {
		return errResp.Error
	}
	if errResp.Error.Info == nil {
		return errors.Annotatef(err, "no error info found in discharge-required response error")
	}
	// It's a discharge-required error, so make an appropriate httpbakery
	// error from it.
	return &httpbakery.Error{
		Message: errResp.Error.Message,
		Code:    httpbakery.ErrDischargeRequired,
		Info: &httpbakery.ErrorInfo{
			Macaroon:     errResp.Error.Info.Macaroon,
			MacaroonPath: errResp.Error.Info.MacaroonPath,
		},
	}
}
*/

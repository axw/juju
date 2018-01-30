// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver_test

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/juju/loggo"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/testing/httptesting"
	"github.com/juju/utils"
	"github.com/juju/utils/clock"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/api"
	"github.com/juju/juju/apiserver"
	"github.com/juju/juju/apiserver/apiserverhttp"
	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/apiserver/observer/fakeobserver"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/pubsub/centralhub"
	"github.com/juju/juju/state"
	statetesting "github.com/juju/juju/state/testing"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/worker/httpserver/stateauthenticator"
	"github.com/juju/juju/worker/workertest"
)

const (
	ownerPassword = "very very secret"
)

// apiserverConfigFixture provides a complete, valid, apiserver.Config.
// Unforunaately this also means that it requires State, at least until
// we update the tests to stop expecting state-based authentication.
//
// apiserverConfigFixture does not run an API server; see apiserverBaseSuite
// for that.
type apiserverConfigFixture struct {
	statetesting.StateSuite
	authenticator *stateauthenticator.Authenticator
	mux           *apiserverhttp.Mux
	tlsConfig     *tls.Config
	config        apiserver.ServerConfig
}

func (s *apiserverConfigFixture) SetUpTest(c *gc.C) {
	s.StateSuite.SetUpTest(c)

	authenticator, err := stateauthenticator.NewAuthenticator(s.StatePool, clock.WallClock)
	c.Assert(err, jc.ErrorIsNil)
	s.authenticator = authenticator
	s.mux = apiserverhttp.NewMux()

	certPool, err := api.CreateCertPool(coretesting.CACert)
	if err != nil {
		panic(err)
	}
	s.tlsConfig = api.NewTLSConfig(certPool)
	s.tlsConfig.ServerName = "juju-apiserver"
	s.tlsConfig.Certificates = []tls.Certificate{*coretesting.ServerTLSCert}
	s.mux = apiserverhttp.NewMux()

	machineTag := names.NewMachineTag("0")
	s.config = apiserver.ServerConfig{
		StatePool:       s.StatePool,
		Authenticator:   s.authenticator,
		Clock:           clock.WallClock,
		Tag:             machineTag,
		DataDir:         c.MkDir(),
		LogDir:          c.MkDir(),
		Hub:             centralhub.New(machineTag),
		Mux:             s.mux,
		NewObserver:     func() observer.Observer { return &fakeobserver.Instance{} },
		RateLimitConfig: apiserver.DefaultRateLimitConfig(),
		UpgradeComplete: func() bool { return true },
		RestoreStatus: func() state.RestoreStatus {
			return state.RestoreNotActive
		},
		RegisterIntrospectionHandlers: func(f func(path string, h http.Handler)) {
			f("navel", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, "gazing")
			}))
		},
	}
}

// apiserverBaseSuite runs an API server.
type apiserverBaseSuite struct {
	apiserverConfigFixture
	server  *httptest.Server
	baseURL *url.URL
}

func (s *apiserverBaseSuite) SetUpTest(c *gc.C) {
	s.apiserverConfigFixture.SetUpTest(c)

	s.server = httptest.NewUnstartedServer(s.mux)
	s.server.TLS = s.tlsConfig
	s.server.StartTLS()
	s.AddCleanup(func(c *gc.C) { s.server.Close() })
	baseURL, err := url.Parse(s.server.URL)
	c.Assert(err, jc.ErrorIsNil)
	s.baseURL = baseURL
	c.Logf("started HTTP server listening on %q", s.server.Listener.Addr())

	server, err := apiserver.NewServer(s.config)
	c.Assert(err, jc.ErrorIsNil)
	s.AddCleanup(func(c *gc.C) {
		workertest.DirtyKill(c, server)
	})

	loggo.GetLogger("juju.apiserver").SetLogLevel(loggo.TRACE)
	u, err := s.State.User(s.Owner)
	c.Assert(err, jc.ErrorIsNil)
	err = u.SetPassword(ownerPassword)
	c.Assert(err, jc.ErrorIsNil)
}

// URL returns a URL for this server with the given path and
// query parameters. The URL scheme will be "https".
func (s *apiserverBaseSuite) URL(path string, queryParams url.Values) *url.URL {
	url := *s.baseURL
	url.Path = path
	url.RawQuery = queryParams.Encode()
	return &url
}

// sendHTTPRequest sends an HTTP request with an appropriate
// username and password.
func (s *apiserverBaseSuite) sendHTTPRequest(c *gc.C, p httpRequestParams) *http.Response {
	p.tag = s.Owner.String()
	p.password = ownerPassword
	return sendHTTPRequest(c, p)
}

// TODO(axw) get rid of below?

func (s *apiserverBaseSuite) newServerNoCleanup(c *gc.C, config apiserver.ServerConfig) *apiserver.Server {
	srv, err := apiserver.NewServer(config)
	c.Assert(err, jc.ErrorIsNil)
	return srv
}

func (s *apiserverBaseSuite) newServer(c *gc.C, config apiserver.ServerConfig) *apiserver.Server {
	srv := s.newServerNoCleanup(c, config)
	s.AddCleanup(func(c *gc.C) {
		workertest.CleanKill(c, srv)
	})
	return srv
}

func (s *apiserverBaseSuite) newServerDirtyKill(c *gc.C, config apiserver.ServerConfig) *apiserver.Server {
	srv := s.newServerNoCleanup(c, config)
	s.AddCleanup(func(c *gc.C) {
		workertest.DirtyKill(c, srv)
	})
	return srv
}

// APIInfo returns an info struct that has the server's address and ca-cert
// populated.
func (s *apiserverBaseSuite) APIInfo(server *apiserver.Server) *api.Info {
	address := s.server.Listener.Addr().String()
	return &api.Info{
		Addrs:  []string{address},
		CACert: coretesting.CACert,
	}
}

func (s *apiserverBaseSuite) openAPIAs(c *gc.C, srv *apiserver.Server, tag names.Tag, password, nonce string, controllerOnly bool) api.Connection {
	apiInfo := s.APIInfo(srv)
	apiInfo.Tag = tag
	apiInfo.Password = password
	apiInfo.Nonce = nonce
	if !controllerOnly {
		apiInfo.ModelTag = s.IAASModel.ModelTag()
	}
	conn, err := api.Open(apiInfo, api.DialOpts{})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(conn, gc.NotNil)
	s.AddCleanup(func(c *gc.C) {
		conn.Close()
	})
	return conn
}

// OpenAPIAsNewMachine creates a new client connection logging in as the
// controller owner. The returned api.Connection should not be closed by the
// caller as a cleanup function has been registered to do that.
func (s *apiserverBaseSuite) OpenAPIAsAdmin(c *gc.C, srv *apiserver.Server) api.Connection {
	return s.openAPIAs(c, srv, s.Owner, ownerPassword, "", false)
}

// OpenAPIAsNewMachine creates a new machine entry that lives in system state,
// and then uses that to open the API. The returned api.Connection should not be
// closed by the caller as a cleanup function has been registered to do that.
// The machine will run the supplied jobs; if none are given, JobHostUnits is assumed.
func (s *apiserverBaseSuite) OpenAPIAsNewMachine(c *gc.C, srv *apiserver.Server, jobs ...state.MachineJob) (api.Connection, *state.Machine) {
	if len(jobs) == 0 {
		jobs = []state.MachineJob{state.JobHostUnits}
	}
	machine, err := s.State.AddMachine("quantal", jobs...)
	c.Assert(err, jc.ErrorIsNil)
	password, err := utils.RandomPassword()
	c.Assert(err, jc.ErrorIsNil)
	err = machine.SetPassword(password)
	c.Assert(err, jc.ErrorIsNil)
	err = machine.SetProvisioned("foo", "fake_nonce", nil)
	c.Assert(err, jc.ErrorIsNil)
	return s.openAPIAs(c, srv, machine.Tag(), password, "fake_nonce", false), machine
}

func dialWebsocketFromURL(c *gc.C, server string, header http.Header) (*websocket.Conn, *http.Response, error) {
	if header == nil {
		header = http.Header{}
	}
	header.Set("Origin", "http://localhost/")
	caCerts := x509.NewCertPool()
	c.Assert(caCerts.AppendCertsFromPEM([]byte(coretesting.CACert)), jc.IsTrue)
	tlsConfig := utils.SecureTLSConfig()
	tlsConfig.RootCAs = caCerts
	tlsConfig.ServerName = "juju-apiserver"
	c.Logf("dialing %v", server)

	dialer := &websocket.Dialer{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsConfig,
	}
	return dialer.Dial(server, header)
}

// httpRequestParams holds parameters for the sendHTTPRequest methods.
type httpRequestParams struct {
	// do is used to make the HTTP request.
	// If it is nil, utils.GetNonValidatingHTTPClient().Do will be used.
	// If the body reader implements io.Seeker,
	// req.Body will also implement that interface.
	do func(req *http.Request) (*http.Response, error)

	// expectError holds the error regexp to match
	// against the error returned from the HTTP Do
	// request. If it is empty, the error is expected to be
	// nil.
	expectError string

	// tag holds the tag to authenticate as.
	tag string

	// password holds the password associated with the tag.
	password string

	// method holds the HTTP method to use for the request.
	method string

	// url holds the URL to send the HTTP request to.
	url string

	// contentType holds the content type of the request.
	contentType string

	// body holds the body of the request.
	body io.Reader

	// extra headers are added to the http header
	extraHeaders map[string]string

	// jsonBody holds an object to be marshaled as JSON
	// as the body of the request. If this is specified, body will
	// be ignored and the Content-Type header will
	// be set to application/json.
	jsonBody interface{}

	// nonce holds the machine nonce to provide in the header.
	nonce string
}

func sendHTTPRequest(c *gc.C, p httpRequestParams) *http.Response {
	c.Logf("sendRequest: %s", p.url)
	hp := httptesting.DoRequestParams{
		Do:          p.do,
		Method:      p.method,
		URL:         p.url,
		Body:        p.body,
		JSONBody:    p.jsonBody,
		Header:      make(http.Header),
		Username:    p.tag,
		Password:    p.password,
		ExpectError: p.expectError,
	}
	if p.contentType != "" {
		hp.Header.Set("Content-Type", p.contentType)
	}
	for key, value := range p.extraHeaders {
		hp.Header.Set(key, value)
	}
	if p.nonce != "" {
		hp.Header.Set(params.MachineNonceHeader, p.nonce)
	}
	if hp.Do == nil {
		hp.Do = utils.GetNonValidatingHTTPClient().Do
	}
	return httptesting.Do(c, hp)
}

func assertResponse(c *gc.C, resp *http.Response, expHTTPStatus int, expContentType string) []byte {
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	c.Assert(err, jc.ErrorIsNil)
	c.Check(resp.StatusCode, gc.Equals, expHTTPStatus, gc.Commentf("body: %s", body))
	ctype := resp.Header.Get("Content-Type")
	c.Assert(ctype, gc.Equals, expContentType)
	return body
}

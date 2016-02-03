// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/utils"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/usermanager"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cmd/envcmd"
	"github.com/juju/juju/environs/configstore"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/network"
)

// NewRegisterCommand returns a command to allow the user to register a controller.
func NewRegisterCommand() cmd.Command {
	return envcmd.WrapBase(&registerCommand{})
}

// registerCommand logs in to a Juju controller and caches the connection
// information.
type registerCommand struct {
	envcmd.JujuCommandBase
	loginAPIOpen api.OpenFunc
	// TODO (thumper): when we support local cert definitions
	// allow the use to specify the user and server address.
	// user      string
	// address   string
	//Server       cmd.FileVar
	ControllerName string
	User           string
	Host           string
	KeepPassword   bool
	Key            []byte
}

var registerDoc = `
register connects to a Juju controller with a secret key, and caches the
information that juju needs to connect to the controller locally.

In order to register a controller, you need to have a user created for
you with "juju add-user". The "juju add-user" command will return a secret
key that you must present to "juju register".

If you have used the 'api-info' command to generate a copy of your current
credentials for a controller, you should use the --keep-password option as it will
mean that you will still be able to connect to the api server from the
computer where you ran api-info.

See Also:
    juju help list-environments
    juju help use-environment
    juju help create-environment
    juju help add-user
    juju help switch
`

// Info implements Command.Info
func (c *registerCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name: "register",
		// TODO(thumper): support user and address options
		// Args: "<name> [<server address>[:<server port>]]"
		Args:    "<name>",
		Purpose: "register a Juju Controller",
		Doc:     registerDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *registerCommand) SetFlags(f *gnuflag.FlagSet) {
	f.StringVar(&c.ControllerName, "name", "", "name to give to the controller")
	f.StringVar(&c.ControllerName, "n", "", "")
	//f.Var(&c.Server, "server", "path to yaml-formatted server file")
	//f.BoolVar(&c.KeepPassword, "keep-password", false, "do not generate a new random password")
}

// SetFlags implements Command.Init.
func (c *registerCommand) Init(args []string) error {
	if c.ControllerName == "" {
		// TODO(axw) prompt user for controller name if not specified.
		return errors.New("specify controller name with --name")
	}
	if len(args) < 2 {
		return errors.New("user@host and controller name must be specified")
	}
	var keyBase64 string
	c.Host, keyBase64, args = args[0], args[1], args[2:]
	if err := cmd.CheckEmpty(args); err != nil {
		return err
	}
	if i := strings.IndexRune(c.Host, '@'); i > 0 {
		c.User, c.Host = c.Host[:i], c.Host[i+1:]
	} else {
		return errors.Errorf("expected user@host")
	}
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return errors.Annotate(err, "decoding key")
	}
	c.Key = key
	return nil
}

func (c *registerCommand) Run(ctx *cmd.Context) error {
	// Generate a random nonce for encrypting the request.
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errors.Trace(err)
	}

	var key [32]byte
	if len(c.Key) != len(key) {
		return errors.NotValidf("secret key")
	}
	copy(key[:], c.Key)

	userTag := names.NewUserTag(c.User).String()
	payloadBytes := append([]byte(userTag), nonce[:]...)

	req := params.SecretKeyLoginRequest{
		Nonce:             nonce[:],
		User:              userTag,
		PayloadCiphertext: secretbox.Seal(nil, payloadBytes, &nonce, &key),
	}
	resp, err := c.secretKeyLogin(req)
	if err != nil {
		return errors.Trace(err)
	}

	if len(resp.Nonce) != len(nonce) {
		return errors.NotValidf("response nonce")
	}
	copy(nonce[:], resp.Nonce)
	payloadBytes, ok := secretbox.Open(nil, resp.PayloadCiphertext, &nonce, &key)
	if !ok {
		return errors.NotValidf("response payload")
	}
	var responsePayload params.SecretKeyLoginResponsePayload
	if err := json.Unmarshal(payloadBytes, &responsePayload); err != nil {
		return errors.Annotate(err, "unmarshalling response payload")
	}
	apiConn, err := c.passwordLogin(responsePayload.CACert, responsePayload.Password)
	if err != nil {
		return errors.Trace(err)
	}
	defer apiConn.Close()

	// Change password.
	password, err := c.changePassword(ctx.Stderr, ctx.Stdin, apiConn)
	if err != nil {
		return errors.Trace(err)
	}
	if _, err := c.cacheConnectionInfo(responsePayload.CACert, password, apiConn); err != nil {
		return errors.Trace(err)
	}
	return errors.Trace(envcmd.SetCurrentController(ctx, c.ControllerName))
}

func (c *registerCommand) secretKeyLogin(request params.SecretKeyLoginRequest) (*params.SecretKeyLoginResponse, error) {
	buf, err := json.Marshal(&request)
	if err != nil {
		return nil, errors.Annotate(err, "marshalling request")
	}
	r := bytes.NewReader(buf)

	// TODO(axw) port needs to be specified by user.
	urlString := fmt.Sprintf("https://%s:%d/credentials", c.Host, 17070)
	httpReq, err := http.NewRequest("POST", urlString, r)
	if err != nil {
		return nil, errors.Trace(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpClient := utils.GetNonValidatingHTTPClient()
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, errors.Trace(err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		var resp params.ErrorResult
		if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
			return nil, errors.Trace(err)
		}
		return nil, resp.Error
	}

	var resp params.SecretKeyLoginResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, errors.Trace(err)
	}
	return &resp, nil
}

func (c *registerCommand) passwordLogin(caCert, password string) (api.Connection, error) {
	info := api.Info{
		Addrs:    []string{net.JoinHostPort(c.Host, "17070")},
		CACert:   caCert,
		Tag:      names.NewUserTag(c.User),
		Password: password,
	}
	loginAPIOpen := c.loginAPIOpen
	if loginAPIOpen == nil {
		loginAPIOpen = c.JujuCommandBase.APIOpen
	}
	apiConn, err := loginAPIOpen(&info, api.DefaultDialOpts())
	if err != nil {
		return nil, errors.Trace(err)
	}
	return apiConn, nil
}

func (c *registerCommand) changePassword(stderr io.Writer, stdin io.Reader, apiConn api.Connection) (string, error) {
	password, err := c.readPassword("Enter password: ", stderr, stdin)
	if err != nil {
		return "", errors.Trace(err)
	}
	passwordConfirmation, err := c.readPassword("Confirm password: ", stderr, stdin)
	if err != nil {
		return "", errors.Trace(err)
	}
	if password != passwordConfirmation {
		return "", errors.Errorf("passwords do not match")
	}
	// TODO(axw) ensure password can't be "". Leave it to the server to
	// check for password rules (special characters, etc.)
	client := usermanager.NewClient(apiConn)
	if err := client.SetPassword(c.User, password); err != nil {
		return "", errors.Trace(err)
	}
	return password, nil
}

func (c *registerCommand) readPassword(prompt string, stderr io.Writer, stdin io.Reader) (string, error) {
	fmt.Fprintf(stderr, "%s", prompt)
	defer stderr.Write([]byte{'\n'})
	if f, ok := stdin.(*os.File); ok && terminal.IsTerminal(int(f.Fd())) {
		password, err := terminal.ReadPassword(int(f.Fd()))
		if err != nil {
			return "", errors.Trace(err)
		}
		return string(password), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil {
		return "", errors.Trace(err)
	}
	return line[:len(line)-1], nil
}

/*
// Run implements Command.Run
func (c *registerCommand) Run(ctx *cmd.Context) error {
	if c.loginAPIOpen == nil {
		c.loginAPIOpen = c.JujuCommandBase.APIOpen
	}

	// TODO(thumper): as we support the user and address
	// change this check here.
	if c.Server.Path == "" {
		return errors.New("no server file specified")
	}

	serverYAML, err := c.Server.Read(ctx)
	if err != nil {
		return errors.Trace(err)
	}

	var serverDetails envcmd.ServerFile
	if err := goyaml.Unmarshal(serverYAML, &serverDetails); err != nil {
		return errors.Trace(err)
	}

	info := api.Info{
		Addrs:  serverDetails.Addresses,
		CACert: serverDetails.CACert,
	}
	var userTag names.UserTag
	if serverDetails.Username != "" {
		// Construct the api.Info struct from the provided values
		// and attempt to connect to the remote server before we do anything else.
		if !names.IsValidUser(serverDetails.Username) {
			return errors.Errorf("%q is not a valid username", serverDetails.Username)
		}

		userTag = names.NewUserTag(serverDetails.Username)
		if !userTag.IsLocal() {
			// Remote users do not have their passwords stored in Juju
			// so we never attempt to change them.
			c.KeepPassword = true
		}
		info.Tag = userTag
	}

	if serverDetails.Password != "" {
		info.Password = serverDetails.Password
	}

	if serverDetails.Password == "" || serverDetails.Username == "" {
		info.UseMacaroons = true
	}
	apiConn, err := c.loginAPIOpen(&info, api.DefaultDialOpts())
	if err != nil {
		return errors.Trace(err)
	}
	defer apiConn.Close()

	// If we get to here, the credentials supplied were sufficient to connect
	// to the Juju Controller and login. Now we cache the details.
	controllerInfo, err := c.cacheConnectionInfo(serverDetails, apiConn)
	if err != nil {
		return errors.Trace(err)
	}
	ctx.Infof("cached connection details as controller %q", c.Name)

	// If we get to here, we have been able to connect to the API server, and
	// also have been able to write the cached information. Now we can change
	// the user's password to a new randomly generated strong password, and
	// update the cached information knowing that the likelihood of failure is
	// minimal.
	if !c.KeepPassword {
		if err := c.updatePassword(ctx, apiConn, userTag, controllerInfo); err != nil {
			return errors.Trace(err)
		}
	}

	return errors.Trace(envcmd.SetCurrentController(ctx, c.Name))
}
*/

func (c *registerCommand) cacheConnectionInfo(caCert, password string, apiConn api.Connection) (configstore.EnvironInfo, error) {
	store, err := configstore.Default()
	if err != nil {
		return nil, errors.Trace(err)
	}
	controllerInfo := store.CreateInfo(c.ControllerName)

	controllerTag, err := apiConn.ControllerTag()
	if err != nil {
		return nil, errors.Wrap(err, errors.New("juju controller too old to support login"))
	}

	connectedAddresses, err := network.ParseHostPorts(apiConn.Addr())
	if err != nil {
		// Should never happen, since we've just connected with it.
		return nil, errors.Annotatef(err, "invalid API address %q", apiConn.Addr())
	}
	addressConnectedTo := connectedAddresses[0]

	addrs, hosts, changed := juju.PrepareEndpointsForCaching(controllerInfo, apiConn.APIHostPorts(), addressConnectedTo)
	if !changed {
		logger.Infof("api addresses: %v", apiConn.APIHostPorts())
		logger.Infof("address connected to: %v", addressConnectedTo)
		return nil, errors.New("no addresses returned from prepare for caching")
	}

	controllerInfo.SetAPICredentials(
		configstore.APICredentials{
			User:     c.User,
			Password: password,
		})

	controllerInfo.SetAPIEndpoint(configstore.APIEndpoint{
		Addresses:  addrs,
		Hostnames:  hosts,
		CACert:     caCert,
		ServerUUID: controllerTag.Id(),
	})

	if err = controllerInfo.Write(); err != nil {
		return nil, errors.Trace(err)
	}
	return controllerInfo, nil
}

/*
func (c *registerCommand) updatePassword(ctx *cmd.Context, conn api.Connection, userTag names.UserTag, controllerInfo configstore.EnvironInfo) error {
	password, err := utils.RandomPassword()
	if err != nil {
		return errors.Annotate(err, "failed to generate random password")
	}

	userManager, err := c.GetUserManager(conn)
	if err != nil {
		return errors.Trace(err)
	}
	if err := userManager.SetPassword(userTag.Name(), password); err != nil {
		errors.Trace(err)
	}
	ctx.Infof("password updated\n")
	creds := controllerInfo.APICredentials()
	creds.Password = password
	controllerInfo.SetAPICredentials(creds)
	if err = controllerInfo.Write(); err != nil {
		return errors.Trace(err)
	}
	return nil
}

// UserManager defines the calls that the Login command makes to the user
// manager client. It is returned by a helper function that is overridden in
// tests.
type UserManager interface {
	SetPassword(username, password string) error
}

func getUserManager(conn api.Connection) (UserManager, error) {
	return usermanager.NewClient(conn), nil
}
*/

// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azureauth_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/authorization"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/mocks"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/provider/azure/internal/ad"
	"github.com/juju/juju/provider/azure/internal/azureauth"
	"github.com/juju/juju/provider/azure/internal/azuretesting"
)

type InteractiveSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&InteractiveSuite{})

func deviceCodeSender() autorest.Sender {
	return azuretesting.NewSenderWithValue(azure.DeviceCode{
		DeviceCode: to.StringPtr("device-code"),
		Interval:   to.Int64Ptr(1), // 1 second between polls
		Message:    to.StringPtr("open your browser, etc."),
	})
}

func tokenSender() autorest.Sender {
	return azuretesting.NewSenderWithValue(azure.Token{
		RefreshToken: "refresh-token",
		ExpiresOn:    fmt.Sprint(time.Now().Add(time.Hour).Unix()),
	})
}

func setPasswordSender() autorest.Sender {
	sender := mocks.NewSender()
	sender.AppendResponse(mocks.NewResponseWithStatus("", http.StatusNoContent))
	return sender
}

func servicePrincipalListSender() autorest.Sender {
	return azuretesting.NewSenderWithValue(ad.ServicePrincipalListResult{
		Value: []ad.ServicePrincipal{{
			ApplicationID: "cbb548f1-5039-4836-af0b-727e8571f6a9",
			ObjectID:      "sp-object-id",
		}},
	})
}

func roleDefinitionListSender() autorest.Sender {
	roleDefinitions := []authorization.RoleDefinition{{
		ID:   to.StringPtr("owner-role-id"),
		Name: to.StringPtr("Owner"),
	}}
	return azuretesting.NewSenderWithValue(authorization.RoleDefinitionListResult{
		Value: &roleDefinitions,
	})
}

func roleAssignmentSender() autorest.Sender {
	return azuretesting.NewSenderWithValue(authorization.RoleAssignment{})
}

func roleAssignmentAlreadyExistsSender() autorest.Sender {
	sender := mocks.NewSender()
	body := mocks.NewBody(`{"error":{"code":"RoleAssignmentExists"}}`)
	sender.AppendResponse(mocks.NewResponseWithBodyAndStatus(body, http.StatusConflict, ""))
	return sender
}

func applicationListSender() autorest.Sender {
	return azuretesting.NewSenderWithValue(ad.ApplicationListResult{
		Value: []ad.Application{{
			ApplicationID: "cbb548f1-5039-4836-af0b-727e8571f6a9",
			ObjectID:      "app-object-id",
		}},
	})
}

func (s *InteractiveSuite) TestInteractive(c *gc.C) {
	uuids := []string{
		"33333333-3333-3333-3333-333333333333", // role assignment ID
		"44444444-4444-4444-4444-444444444444", // password
		"55555555-5555-5555-5555-555555555555", // password key ID
	}
	newUUID := func() (utils.UUID, error) {
		uuid, err := utils.UUIDFromString(uuids[0])
		if err != nil {
			return utils.UUID{}, err
		}
		uuids = uuids[1:]
		return uuid, nil
	}

	var requests []*http.Request
	senders := azuretesting.Senders{
		oauthConfigSender(),
		deviceCodeSender(),
		tokenSender(), // CheckForUserCompletion returns a token.

		// Token.Refresh returns a token. We do this
		// twice: once for ARM, and once for AAD.
		tokenSender(),
		tokenSender(),

		servicePrincipalListSender(),
		roleDefinitionListSender(),
		roleAssignmentSender(),
		applicationListSender(),
		setPasswordSender(),
	}

	var stderr bytes.Buffer
	subscriptionId := "22222222-2222-2222-2222-222222222222"
	appId, password, err := azureauth.InteractiveCreateServicePrincipal(
		&stderr,
		&senders,
		azuretesting.RequestRecorder(&requests),
		"https://arm.invalid",
		"https://graph.invalid",
		subscriptionId,
		newUUID,
	)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(appId, gc.Equals, "cbb548f1-5039-4836-af0b-727e8571f6a9")
	c.Assert(password, gc.Equals, "44444444-4444-4444-4444-444444444444")
	c.Assert(stderr.String(), gc.Equals, `
Initiating interactive authentication.

open your browser, etc.

Assigning Owner role to service principal.
Setting password for service principal.
`[1:])

	// Token refreshes don't go through the inspectors.
	c.Assert(requests, gc.HasLen, 8)
	c.Check(requests[0].URL.Path, gc.Equals, "/subscriptions/22222222-2222-2222-2222-222222222222")
	c.Check(requests[1].URL.Path, gc.Equals, "/11111111-1111-1111-1111-111111111111/oauth2/devicecode")
	c.Check(requests[2].URL.Path, gc.Equals, "/11111111-1111-1111-1111-111111111111/oauth2/token")
	c.Check(requests[3].URL.Path, gc.Equals, "/11111111-1111-1111-1111-111111111111/servicePrincipals")
	c.Check(requests[4].URL.Path, gc.Equals, "/subscriptions/22222222-2222-2222-2222-222222222222/providers/Microsoft.Authorization/roleDefinitions")
	c.Check(requests[5].URL.Path, gc.Equals, "/subscriptions/22222222-2222-2222-2222-222222222222/providers/Microsoft.Authorization/roleAssignments/33333333-3333-3333-3333-333333333333")
	c.Check(requests[6].URL.Path, gc.Equals, "/11111111-1111-1111-1111-111111111111/applications")
	c.Check(requests[7].URL.Path, gc.Equals, "/11111111-1111-1111-1111-111111111111/applications/app-object-id")

	// The last request is to set the password. Check that the password
	// returned from the function is the same as the one set in the
	// request.
	var setPasswordCredentials ad.Application
	err = json.NewDecoder(requests[7].Body).Decode(&setPasswordCredentials)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(setPasswordCredentials.PasswordCredentials, gc.HasLen, 1)

	startDate := setPasswordCredentials.PasswordCredentials[0].StartDate
	endDate := setPasswordCredentials.PasswordCredentials[0].EndDate
	c.Assert(startDate.IsZero(), jc.IsFalse)
	c.Assert(endDate.Sub(startDate), gc.Equals, 365*24*time.Hour)

	setPasswordCredentials.PasswordCredentials[0].StartDate = time.Time{}
	setPasswordCredentials.PasswordCredentials[0].EndDate = time.Time{}
	c.Assert(setPasswordCredentials, jc.DeepEquals, ad.Application{
		PasswordCredentials: []ad.PasswordCredential{{
			KeyId: "55555555-5555-5555-5555-555555555555",
			Value: "44444444-4444-4444-4444-444444444444",
		}},
	})
}

func (s *InteractiveSuite) TestInteractiveRoleAssignmentAlreadyExists(c *gc.C) {
	var requests []*http.Request
	senders := azuretesting.Senders{
		oauthConfigSender(),
		deviceCodeSender(),
		tokenSender(),
		tokenSender(),
		tokenSender(),
		servicePrincipalListSender(),
		roleDefinitionListSender(),
		roleAssignmentAlreadyExistsSender(),
		applicationListSender(),
		setPasswordSender(),
	}
	_, _, err := azureauth.InteractiveCreateServicePrincipal(
		ioutil.Discard,
		&senders,
		azuretesting.RequestRecorder(&requests),
		"https://arm.invalid",
		"https://graph.invalid",
		"22222222-2222-2222-2222-222222222222",
		utils.NewUUID,
	)
	c.Assert(err, jc.ErrorIsNil)
}

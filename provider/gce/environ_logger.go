package gce

import (
	"context"
	"fmt"
	"strings"

	"cloud.google.com/go/logging"
	goauth2 "golang.org/x/oauth2/google"
	"google.golang.org/api/option"

	"github.com/juju/errors"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/logfwd"
	"github.com/juju/juju/provider/gce/google"
	"github.com/juju/loggo"
)

func (env *environ) LogForwarder() (logfwd.RecordSendCloser, error) {
	// TODO(axw) extract a function from newEnviron to do this.
	credAttrs := env.cloud.Credential.Attributes()
	if env.cloud.Credential.AuthType() == cloud.JSONFileAuthType {
		contents := credAttrs[credAttrFile]
		credential, err := parseJSONAuthFile(strings.NewReader(contents))
		if err != nil {
			return nil, errors.Trace(err)
		}
		credAttrs = credential.Attributes()
	}
	credential := &google.Credentials{
		ClientID:    credAttrs[credAttrClientID],
		ProjectID:   credAttrs[credAttrProjectID],
		ClientEmail: credAttrs[credAttrClientEmail],
		PrivateKey:  []byte(credAttrs[credAttrPrivateKey]),
	}
	jsonKey, err := google.JSONKeyFromCredentials(credential)
	if err != nil {
		return nil, errors.Trace(err)
	}

	projectID := env.cloud.Credential.Attributes()[credAttrProjectID]
	jwtConfig, err := goauth2.JWTConfigFromJSON(jsonKey, logging.WriteScope)
	if err != nil {
		return nil, errors.Trace(err)
	}
	ctx := context.Background()
	client, err := logging.NewClient(
		ctx, projectID,
		option.WithTokenSource(jwtConfig.TokenSource(ctx)),
	)
	if err != nil {
		return nil, errors.Annotate(err, "creating logging client")
	}
	return stackdriverLogger{client}, nil
}

type stackdriverLogger struct {
	client *logging.Client
}

func (l stackdriverLogger) Close() error {
	return l.client.Close()
}

func (l stackdriverLogger) Send(records []logfwd.Record) error {
	logger := l.client.Logger("juju")
	for _, r := range records {
		var severity logging.Severity
		switch r.Level {
		case loggo.CRITICAL:
			severity = logging.Critical
		case loggo.ERROR:
			severity = logging.Error
		case loggo.WARNING:
			severity = logging.Warning
		case loggo.INFO:
			severity = logging.Info
		case loggo.DEBUG, loggo.TRACE:
			severity = logging.Debug
		default:
			severity = logging.Default
		}
		labels := map[string]string{
			"source":   r.Location.String(),
			"origin":   r.Origin.Name,
			"hostname": r.Origin.Hostname,
			"software": fmt.Sprintf(
				"%d:%s:%s",
				r.Origin.Software.PrivateEnterpriseNumber,
				r.Origin.Software.Name,
				r.Origin.Software.Version,
			),
			// TODO(axw) the forwarder runs per model,
			// so controller and model should be added
			// to the logger as common labels.
			"controller": r.Origin.ControllerUUID,
			"model":      r.Origin.ModelUUID,
		}
		logger.Log(logging.Entry{
			Timestamp: r.Timestamp,
			Severity:  severity,
			InsertID:  fmt.Sprint(r.ID),
			Payload:   r.Message,
			Labels:    labels,
		})
	}
	return nil
}

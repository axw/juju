// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"fmt"
	"io"
	"net/http"

	"github.com/juju/errors"
	"github.com/juju/utils"

	"github.com/juju/juju/version"
)

// ToolsDownloader is used to download a tools tarball
// given a binary version.
type ToolsDownloader interface {
	// DownloadTools downloads the tools with the specified
	// binary version.
	DownloadTools(v version.Binary) (io.ReadCloser, error)
}

type toolsDownloader struct {
	serverRoot string
}

// NewToolsDownloader returns a ToolsDownloader that downloads
// tools from the API server with the specified HTTPS URL root.
func NewToolsDownloader(serverRoot string) ToolsDownloader {
	return toolsDownloader{serverRoot}
}

func (d toolsDownloader) DownloadTools(v version.Binary) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/tools/%s", d.serverRoot, v)
	// The reader MUST verify the tools' hash, so there is no
	// need to validate the peer. We cannot anyway: see http://pad.lv/1261780.
	resp, err := utils.GetNonValidatingHTTPClient().Get(url)
	if err != nil {
		return nil, errors.Annotate(err, "cannot download tools")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, errors.Errorf("bad HTTP response: %v", resp.Status)
	}
	return resp.Body, nil
}

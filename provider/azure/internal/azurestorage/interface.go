// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azurestorage

import (
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/juju/errors"
)

// Client is an interface providing access to Azure storage services.
type Client interface {
	// GetBlobService returns a BlobStorageClient which can operate
	// on the blob service of the storage account.
	GetBlobService() BlobStorageClient
}

// BlobStorageClient is an interface providing access to Azure blob storage.
//
// This interface the subet of functionality provided by
// https://godoc.org/github.com/Azure/azure-sdk-for-go/storage#BlobStorageClient
// that is required by Juju.
type BlobStorageClient interface {
	// GetContainerReference returns a Container object for the specified container name.
	GetContainerReference(name string) Container
}

// Container provides access to an Azure storage container.
type Container interface {
	// Blobs returns the blobs in the container.
	//
	// See https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/List-Blobs
	Blobs() ([]Blob, error)

	// Blob returns a Blob object for the specified blob name.
	Blob(name string) Blob
}

// Blob provides access to an Azure storage blob.
type Blob interface {
	// DeleteIfExists deletes the given blob from the specified container If the
	// blob is deleted with this call, returns true. Otherwise returns false.
	//
	// See https://docs.microsoft.com/en-us/rest/api/storageservices/fileservices/Delete-Blob
	DeleteIfExists(*storage.DeleteBlobOptions) (bool, error)
}

// NewClientFunc is the type of the NewClient function.
type NewClientFunc func(
	accountName, accountKey, blobServiceBaseURL, apiVersion string,
	useHTTPS bool,
) (Client, error)

// NewClient returns a Client that is backed by a storage.Client created with
// storage.NewClient
func NewClient(accountName, accountKey, blobServiceBaseURL, apiVersion string, useHTTPS bool) (Client, error) {
	client, err := storage.NewClient(accountName, accountKey, blobServiceBaseURL, apiVersion, useHTTPS)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return clientWrapper{client}, nil
}

type clientWrapper struct {
	storage.Client
}

func (w clientWrapper) GetBlobService() BlobStorageClient {
	return &blobStorageClient{w.Client.GetBlobService()}
}

type blobStorageClient struct {
	storage.BlobStorageClient
}

func (c *blobStorageClient) GetContainerReference(name string) Container {
	return container{c.BlobStorageClient.GetContainerReference(name)}
}

type container struct {
	*storage.Container
}

func (c container) Blobs() ([]Blob, error) {
	//TODO(axw) handle pagination.
	resp, err := c.Container.ListBlobs(storage.ListBlobsParameters{})
	if err != nil {
		return nil, errors.Trace(err)
	}
	blobs := make([]Blob, len(resp.Blobs))
	for i := range blobs {
		blobs[i] = &resp.Blobs[i]
	}
	return blobs, nil
}

func (c container) Blob(name string) Blob {
	return c.Container.GetBlobReference(name)
}

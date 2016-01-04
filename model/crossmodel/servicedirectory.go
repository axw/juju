// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package crossmodel

import (
	"github.com/juju/errors"
)

type ServiceOfferLister interface {
	// List offers returns the offers from the specified directory satisfying the specified filter.
	ListOffers(directory string, filter ...ServiceOfferFilter) ([]ServiceOffer, error)
}

// ServiceOfferForURL returns a service offer for the specified URL,
// checking that the specified user (if non-empty) has been granted
// access to use that offer.
func ServiceOfferForURL(offers ServiceOfferLister, urlStr string, user string) (ServiceOffer, error) {
	url, err := ParseServiceURL(urlStr)
	if err != nil {
		return ServiceOffer{}, err
	}
	filter := ServiceOfferFilter{
		ServiceOffer: ServiceOffer{ServiceURL: urlStr},
	}
	if user != "" {
		filter.AllowedUsers = []string{user}
	}
	results, err := offers.ListOffers(url.Directory, filter)
	if err != nil {
		return ServiceOffer{}, errors.Trace(err)
	}
	if len(results) == 0 {
		return ServiceOffer{}, errors.NotFoundf("service offer at %q", url)
	}
	if len(results) != 1 {
		return ServiceOffer{}, errors.Errorf("expected 1 result, got %d", len(results))
	}
	return results[0], nil
}

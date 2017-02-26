// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package httpserver

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"strings"
	"sync"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"github.com/juju/errors"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/utils"

	"gopkg.in/tomb.v1"
)

type certManager struct {
	tomb        tomb.Tomb
	certChanged <-chan params.StateServingInfo

	mu           sync.RWMutex
	cert         *tls.Certificate
	certDNSNames []string
}

func newCertManager(certChanged <-chan params.StateServingInfo) *certManager {
	m := &certManager{
		certChanged: certChanged,
	}
	go func() {
		defer m.tomb.Done()
		m.tomb.Kill(m.loop())
	}()
	return m
}

// Kill is part of the worker.Worker interface.
func (m *certManager) Kill() {
	m.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (m *certManager) Wait() error {
	return m.tomb.Wait()
}

func (m *certManager) loop() error {
	for {
		select {
		case info := <-m.certChanged:
			if info.Cert == "" {
				break
			}
			logger.Infof("received TLS certificate")
			if err := m.updateCert(info.Cert, info.PrivateKey); err != nil {
				// TODO(axw) why are we not bouncing on this error?
				logger.Errorf("cannot update certificate: %v", err)
			}
		case <-m.tomb.Dying():
			return tomb.ErrDying
		}
	}
}

func (m *certManager) newTLSConfig(
	autocertDNSName, autocertURL string,
	autocertCache autocert.Cache,
) *tls.Config {
	tlsConfig := utils.SecureTLSConfig()
	if autocertDNSName == "" {
		// No official DNS name, no autocert certificate.
		tlsConfig.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, _ := m.localCert(clientHello.ServerName)
			return cert, nil
		}
		return tlsConfig
	}

	am := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocertCache,
		HostPolicy: autocert.HostWhitelist(autocertDNSName),
	}
	if autocertURL != "" {
		am.Client = &acme.Client{
			DirectoryURL: autocertURL,
		}
	}
	tlsConfig.GetCertificate = func(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		logger.Infof("getting certificate for server name %q", clientHello.ServerName)
		// Get the locally created certificate and whether it's appropriate
		// for the SNI name. If not, we'll try to get an acme cert and
		// fall back to the local certificate if that fails.
		cert, shouldUse := m.localCert(clientHello.ServerName)
		if shouldUse {
			return cert, nil
		}
		acmeCert, err := am.GetCertificate(clientHello)
		if err == nil {
			return acmeCert, nil
		}
		logger.Errorf("cannot get autocert certificate for %q: %v", clientHello.ServerName, err)
		return cert, nil
	}
	return tlsConfig

}

func (m *certManager) updateCert(cert, key string) error {
	tlsCert, err := tls.X509KeyPair([]byte(cert), []byte(key))
	if err != nil {
		return errors.Annotatef(err, "cannot create new TLS certificate")
	}
	x509Cert, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return errors.Annotatef(err, "parsing x509 cert")
	}
	var addr []string
	for _, ip := range x509Cert.IPAddresses {
		addr = append(addr, ip.String())
	}
	logger.Infof("new certificate addresses: %v", strings.Join(addr, ", "))

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cert = &tlsCert
	m.certDNSNames = x509Cert.DNSNames
	return nil
}

func (m *certManager) localCert(serverName string) (*tls.Certificate, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if net.ParseIP(serverName) != nil {
		// IP address connections always use the local certificate.
		return m.cert, true
	}
	if !strings.Contains(serverName, ".") {
		// If the server name doesn't contain a period there's no
		// way we can obtain a certificate for it.
		// This applies to the common case where "juju-apiserver" is
		// used as the server name.
		return m.cert, true
	}
	// Perhaps the server name is explicitly mentioned by the server certificate.
	for _, name := range m.certDNSNames {
		if name == serverName {
			return m.cert, true
		}
	}
	return m.cert, false
}

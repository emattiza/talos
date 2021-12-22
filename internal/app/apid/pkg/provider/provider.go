// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package provider provides TLS config for client & server.
package provider

import (
	"context"
	stdlibtls "crypto/tls"
	"fmt"
	"log"
	"sync"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/talos-systems/crypto/tls"

	"github.com/talos-systems/talos/pkg/machinery/resources/secrets"
)

// TLSConfig provides client & server TLS configs for apid.
type TLSConfig struct {
	certificateProvider *certificateProvider
}

// NewTLSConfig builds provider from configuration and endpoints.
func NewTLSConfig(resources state.State) (*TLSConfig, error) {
	watchCh := make(chan state.Event)

	if err := resources.Watch(context.TODO(), resource.NewMetadata(secrets.NamespaceName, secrets.APIType, secrets.APIID, resource.VersionUndefined), watchCh); err != nil {
		return nil, fmt.Errorf("error setting up watch: %w", err)
	}

	// wait for the first event to set up certificate provider
	provider := &certificateProvider{}

	for {
		event := <-watchCh
		if event.Type == state.Destroyed {
			continue
		}

		apiCerts := event.Resource.(*secrets.API) //nolint:errcheck,forcetypeassert

		if err := provider.Update(apiCerts); err != nil {
			return nil, err
		}

		break
	}

	go func() {
		for {
			event := <-watchCh
			if event.Type == state.Destroyed {
				continue
			}

			apiCerts := event.Resource.(*secrets.API) //nolint:errcheck,forcetypeassert

			if err := provider.Update(apiCerts); err != nil {
				log.Printf("failed updating cert: %v", err)
			}
		}
	}()

	return &TLSConfig{
		certificateProvider: provider,
	}, nil
}

// ServerConfig generates server-side tls.Config.
func (tlsConfig *TLSConfig) ServerConfig() (*stdlibtls.Config, error) {
	ca, err := tlsConfig.certificateProvider.GetCA()
	if err != nil {
		return nil, fmt.Errorf("failed to get root CA: %w", err)
	}

	return tls.New(
		tls.WithClientAuthType(tls.Mutual),
		tls.WithCACertPEM(ca),
		tls.WithServerCertificateProvider(tlsConfig.certificateProvider),
	)
}

// ClientConfig generates client-side tls.Config.
func (tlsConfig *TLSConfig) ClientConfig() (*stdlibtls.Config, error) {
	ca, err := tlsConfig.certificateProvider.GetCA()
	if err != nil {
		return nil, fmt.Errorf("failed to get root CA: %w", err)
	}

	return tls.New(
		tls.WithClientAuthType(tls.Mutual),
		tls.WithCACertPEM(ca),
		tls.WithClientCertificateProvider(tlsConfig.certificateProvider),
	)
}

type certificateProvider struct {
	mu sync.Mutex

	apiCerts               *secrets.API
	clientCert, serverCert *stdlibtls.Certificate
}

func (p *certificateProvider) Update(apiCerts *secrets.API) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.apiCerts = apiCerts

	serverCert, err := stdlibtls.X509KeyPair(p.apiCerts.TypedSpec().Server.Crt, p.apiCerts.TypedSpec().Server.Key)
	if err != nil {
		return fmt.Errorf("failed to parse server cert and key into a TLS Certificate: %w", err)
	}

	p.serverCert = &serverCert

	clientCert, err := stdlibtls.X509KeyPair(p.apiCerts.TypedSpec().Client.Crt, p.apiCerts.TypedSpec().Client.Key)
	if err != nil {
		return fmt.Errorf("failed to parse client cert and key into a TLS Certificate: %w", err)
	}

	p.clientCert = &clientCert

	return nil
}

func (p *certificateProvider) GetCA() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.apiCerts.TypedSpec().CA.Crt, nil
}

func (p *certificateProvider) GetCertificate(h *stdlibtls.ClientHelloInfo) (*stdlibtls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.serverCert, nil
}

func (p *certificateProvider) GetClientCertificate(*stdlibtls.CertificateRequestInfo) (*stdlibtls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.clientCert, nil
}

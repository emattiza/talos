// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package basic

import (
	"crypto/tls"
	stdx509 "crypto/x509"

	"github.com/talos-systems/crypto/x509"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Credentials describes an authorization method.
type Credentials interface {
	credentials.PerRPCCredentials

	UnaryInterceptor() grpc.UnaryServerInterceptor
}

// NewConnection initializes a grpc.ClientConn configured for basic
// authentication.
func NewConnection(address string, creds credentials.PerRPCCredentials, ca *x509.PEMEncodedCertificateAndKey) (conn *grpc.ClientConn, err error) {
	tlsConfig := &tls.Config{}

	if ca == nil {
		tlsConfig.InsecureSkipVerify = true
	} else {
		tlsConfig.RootCAs = stdx509.NewCertPool()
		tlsConfig.RootCAs.AppendCertsFromPEM(ca.Crt)
	}

	grpcOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithPerRPCCredentials(creds),
	}

	conn, err = grpc.Dial(address, grpcOpts...)
	if err != nil {
		return
	}

	return conn, nil
}

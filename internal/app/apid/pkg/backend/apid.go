// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package backend

import (
	"context"
	"fmt"
	"sync"

	"github.com/talos-systems/grpc-proxy/proxy"
	"github.com/talos-systems/net"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/talos-systems/talos/pkg/grpc/middleware/authz"
	"github.com/talos-systems/talos/pkg/machinery/api/common"
	"github.com/talos-systems/talos/pkg/machinery/constants"
	"github.com/talos-systems/talos/pkg/machinery/proto"
)

// APID backend performs proxying to another apid instance.
//
// Backend authenticates itself using given grpc credentials.
type APID struct {
	target string
	creds  credentials.TransportCredentials

	mu   sync.Mutex
	conn *grpc.ClientConn
}

// NewAPID creates new instance of APID backend.
func NewAPID(target string, creds credentials.TransportCredentials) (*APID, error) {
	// perform very basic validation on target, trying to weed out empty addresses or addresses with the port appended
	if target == "" || net.AddressContainsPort(target) {
		return nil, fmt.Errorf("invalid target %q", target)
	}

	return &APID{
		target: target,
		creds:  creds,
	}, nil
}

func (a *APID) String() string {
	return a.target
}

// GetConnection returns a grpc connection to the backend.
func (a *APID) GetConnection(ctx context.Context) (context.Context, *grpc.ClientConn, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	md = md.Copy()

	authz.SetMetadata(md, authz.GetRoles(ctx))

	if authority := md[":authority"]; len(authority) > 0 {
		md.Set("proxyfrom", authority...)
	} else {
		md.Set("proxyfrom", "unknown")
	}

	delete(md, ":authority")
	delete(md, "nodes")

	outCtx := metadata.NewOutgoingContext(ctx, md)

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn != nil {
		return outCtx, a.conn, nil
	}

	var err error
	a.conn, err = grpc.DialContext(
		ctx,
		fmt.Sprintf("%s:%d", net.FormatAddress(a.target), constants.ApidPort),
		grpc.WithTransportCredentials(a.creds),
		grpc.WithCodec(proxy.Codec()), //nolint:staticcheck
	)

	return outCtx, a.conn, err
}

// AppendInfo is called to enhance response from the backend with additional data.
//
// AppendInfo enhances upstream response with node metadata (target).
//
// This method depends on grpc protobuf response structure, each response should
// look like:
//
//   message SomeResponse {
//     repeated SomeReply messages = 1; // please note field ID == 1
//   }
//
//   message SomeReply {
//	   common.Metadata metadata = 1;
//     <other fields go here ...>
//   }
//
// As 'SomeReply' is repeated in 'SomeResponse', if we concatenate protobuf representation
// of several 'SomeResponse' messages, we still get valid 'SomeResponse' representation but with more
// entries (feature of protobuf binary representation).
//
// If we look at binary representation of any unary 'SomeResponse' message, it will always contain one
// protobuf field with field ID 1 (see above) and type 2 (embedded message SomeReply is encoded
// as string with length). So if we want to add fields to 'SomeReply', we can simply read field
// header, adjust length for new 'SomeReply' representation, and prepend new field header.
//
// At the same time, we can add 'common.Metadata' structure to 'SomeReply' by simply
// appending or prepending 'common.Metadata' as a single field. This requires 'metadata'
// field to be not defined in original response. (This is due to the fact that protobuf message
// representation is concatenation of each field representation).
//
// To build only single field (Metadata) we use helper message which contains exactly this
// field with same field ID as in every other 'SomeReply':
//
//   message Empty {
//     common.Metadata metadata = 1;
//	}
//
// As streaming replies are not wrapped into 'SomeResponse' with 'repeated', handling is simpler: we just
// need to append Empty with details.
//
// So AppendInfo does the following: validates that response contains field ID 1 encoded as string,
// cuts field header, rest is representation of some reply. Marshal 'Empty' as protobuf,
// which builds 'common.Metadata' field, append it to original response message, build new header
// for new length of some response, and add back new field header.
func (a *APID) AppendInfo(streaming bool, resp []byte) ([]byte, error) {
	payload, err := proto.Marshal(&common.Empty{
		Metadata: &common.Metadata{
			Hostname: a.target,
		},
	})

	if streaming {
		return append(resp, payload...), err
	}

	const (
		metadataField = 1 // field number in proto definition for repeated response
		metadataType  = 2 // "string" for embedded messages
	)

	// decode protobuf embedded header

	typ, n1 := protowire.ConsumeVarint(resp)
	if n1 < 0 {
		return nil, protowire.ParseError(n1)
	}

	_, n2 := protowire.ConsumeVarint(resp[n1:]) // length
	if n2 < 0 {
		return nil, protowire.ParseError(n2)
	}

	if typ != (metadataField<<3)|metadataType {
		return nil, fmt.Errorf("unexpected message format: %d", typ)
	}

	if n1+n2 > len(resp) {
		return nil, fmt.Errorf("unexpected message size: %d", len(resp))
	}

	// cut off embedded message header
	resp = resp[n1+n2:]
	// build new embedded message header
	prefix := protowire.AppendVarint(
		protowire.AppendVarint(nil, (metadataField<<3)|metadataType),
		uint64(len(resp)+len(payload)),
	)
	resp = append(prefix, resp...)

	return append(resp, payload...), err
}

// BuildError is called to convert error from upstream into response field.
//
// BuildError converts upstream error into message from upstream, so that multiple
// successful and failure responses might be returned.
//
// This simply relies on the fact that any response contains 'Empty' message.
// So if 'Empty' is unmarshalled into any other reply message, all the fields
// are undefined but 'Metadata':
//
//   message Empty {
//    common.Metadata metadata = 1;
//	}
//
//  message EmptyResponse {
//    repeated Empty messages = 1;
// }
//
// Streaming responses are not wrapped into Empty, so we simply marshall EmptyResponse
// message.
func (a *APID) BuildError(streaming bool, err error) ([]byte, error) {
	var resp proto.Message = &common.Empty{
		Metadata: &common.Metadata{
			Hostname: a.target,
			Error:    err.Error(),
			Status:   status.Convert(err).Proto(),
		},
	}

	if !streaming {
		resp = &common.EmptyResponse{
			Messages: []*common.Empty{
				resp.(*common.Empty),
			},
		}
	}

	return proto.Marshal(resp)
}

// Close connection.
func (a *APID) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn != nil {
		a.conn.Close() //nolint:errcheck
		a.conn = nil
	}
}

// Code generated by "enumer -type=RouteProtocol -linecomment -text"; DO NOT EDIT.

package nethelpers

import (
	"fmt"
)

const (
	_RouteProtocolName_0 = "unspecredirectkernelbootstatic"
	_RouteProtocolName_1 = "ramrtzebrabirddnroutedxorpntkdhcpmrtdkeepalived"
	_RouteProtocolName_2 = "babel"
	_RouteProtocolName_3 = "openr"
	_RouteProtocolName_4 = "bgpisisospfrip"
	_RouteProtocolName_5 = "eigrp"
)

var (
	_RouteProtocolIndex_0 = [...]uint8{0, 6, 14, 20, 24, 30}
	_RouteProtocolIndex_1 = [...]uint8{0, 2, 5, 10, 14, 22, 26, 29, 33, 37, 47}
	_RouteProtocolIndex_2 = [...]uint8{0, 5}
	_RouteProtocolIndex_3 = [...]uint8{0, 5}
	_RouteProtocolIndex_4 = [...]uint8{0, 3, 7, 11, 14}
	_RouteProtocolIndex_5 = [...]uint8{0, 5}
)

func (i RouteProtocol) String() string {
	switch {
	case 0 <= i && i <= 4:
		return _RouteProtocolName_0[_RouteProtocolIndex_0[i]:_RouteProtocolIndex_0[i+1]]
	case 9 <= i && i <= 18:
		i -= 9
		return _RouteProtocolName_1[_RouteProtocolIndex_1[i]:_RouteProtocolIndex_1[i+1]]
	case i == 42:
		return _RouteProtocolName_2
	case i == 99:
		return _RouteProtocolName_3
	case 186 <= i && i <= 189:
		i -= 186
		return _RouteProtocolName_4[_RouteProtocolIndex_4[i]:_RouteProtocolIndex_4[i+1]]
	case i == 192:
		return _RouteProtocolName_5
	default:
		return fmt.Sprintf("RouteProtocol(%d)", i)
	}
}

var _RouteProtocolValues = []RouteProtocol{0, 1, 2, 3, 4, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 42, 99, 186, 187, 188, 189, 192}

var _RouteProtocolNameToValueMap = map[string]RouteProtocol{
	_RouteProtocolName_0[0:6]:   0,
	_RouteProtocolName_0[6:14]:  1,
	_RouteProtocolName_0[14:20]: 2,
	_RouteProtocolName_0[20:24]: 3,
	_RouteProtocolName_0[24:30]: 4,
	_RouteProtocolName_1[0:2]:   9,
	_RouteProtocolName_1[2:5]:   10,
	_RouteProtocolName_1[5:10]:  11,
	_RouteProtocolName_1[10:14]: 12,
	_RouteProtocolName_1[14:22]: 13,
	_RouteProtocolName_1[22:26]: 14,
	_RouteProtocolName_1[26:29]: 15,
	_RouteProtocolName_1[29:33]: 16,
	_RouteProtocolName_1[33:37]: 17,
	_RouteProtocolName_1[37:47]: 18,
	_RouteProtocolName_2[0:5]:   42,
	_RouteProtocolName_3[0:5]:   99,
	_RouteProtocolName_4[0:3]:   186,
	_RouteProtocolName_4[3:7]:   187,
	_RouteProtocolName_4[7:11]:  188,
	_RouteProtocolName_4[11:14]: 189,
	_RouteProtocolName_5[0:5]:   192,
}

// RouteProtocolString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func RouteProtocolString(s string) (RouteProtocol, error) {
	if val, ok := _RouteProtocolNameToValueMap[s]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to RouteProtocol values", s)
}

// RouteProtocolValues returns all values of the enum
func RouteProtocolValues() []RouteProtocol {
	return _RouteProtocolValues
}

// IsARouteProtocol returns "true" if the value is listed in the enum definition. "false" otherwise
func (i RouteProtocol) IsARouteProtocol() bool {
	for _, v := range _RouteProtocolValues {
		if i == v {
			return true
		}
	}
	return false
}

// MarshalText implements the encoding.TextMarshaler interface for RouteProtocol
func (i RouteProtocol) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for RouteProtocol
func (i *RouteProtocol) UnmarshalText(text []byte) error {
	var err error
	*i, err = RouteProtocolString(string(text))
	return err
}

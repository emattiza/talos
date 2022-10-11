// Code generated by "enumer -type=ADSelect -linecomment -text"; DO NOT EDIT.

package nethelpers

import (
	"fmt"
)

const _ADSelectName = "stablebandwidthcount"

var _ADSelectIndex = [...]uint8{0, 6, 15, 20}

func (i ADSelect) String() string {
	if i >= ADSelect(len(_ADSelectIndex)-1) {
		return fmt.Sprintf("ADSelect(%d)", i)
	}
	return _ADSelectName[_ADSelectIndex[i]:_ADSelectIndex[i+1]]
}

var _ADSelectValues = []ADSelect{0, 1, 2}

var _ADSelectNameToValueMap = map[string]ADSelect{
	_ADSelectName[0:6]:   0,
	_ADSelectName[6:15]:  1,
	_ADSelectName[15:20]: 2,
}

// ADSelectString retrieves an enum value from the enum constants string name.
// Throws an error if the param is not part of the enum.
func ADSelectString(s string) (ADSelect, error) {
	if val, ok := _ADSelectNameToValueMap[s]; ok {
		return val, nil
	}
	return 0, fmt.Errorf("%s does not belong to ADSelect values", s)
}

// ADSelectValues returns all values of the enum
func ADSelectValues() []ADSelect {
	return _ADSelectValues
}

// IsAADSelect returns "true" if the value is listed in the enum definition. "false" otherwise
func (i ADSelect) IsAADSelect() bool {
	for _, v := range _ADSelectValues {
		if i == v {
			return true
		}
	}
	return false
}

// MarshalText implements the encoding.TextMarshaler interface for ADSelect
func (i ADSelect) MarshalText() ([]byte, error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for ADSelect
func (i *ADSelect) UnmarshalText(text []byte) error {
	var err error
	*i, err = ADSelectString(string(text))
	return err
}

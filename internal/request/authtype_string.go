// Code generated by "stringer -type AuthType"; DO NOT EDIT.

package request

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[BearerToken-0]
	_ = x[TLSCertificate-1]
	_ = x[Anonymous-2]
}

const _AuthType_name = "BearerTokenTLSCertificateAnonymous"

var _AuthType_index = [...]uint8{0, 11, 25, 34}

func (i AuthType) String() string {
	if i < 0 || i >= AuthType(len(_AuthType_index)-1) {
		return "AuthType(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _AuthType_name[_AuthType_index[i]:_AuthType_index[i+1]]
}

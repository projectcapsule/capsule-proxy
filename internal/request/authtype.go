package request

//go:generate stringer -type AuthType

type AuthType int

const (
	BearerToken AuthType = iota
	TLSCertificate
	Anonymous
)

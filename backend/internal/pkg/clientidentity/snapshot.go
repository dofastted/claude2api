package clientidentity

// IdentitySnapshot keeps the client identity fields together so later header,
// billing, identity, and TLS injection can consume one consistent source.
type IdentitySnapshot struct {
	Headers        map[string]string
	VersionFields  VersionFields
	TLSProfileName string
}

type VersionFields struct {
	CLIVersion string
	SDKVersion string
	CCVersion  string
	CCFP       string
}

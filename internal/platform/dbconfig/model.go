package dbconfig

import "net/url"

type Mode string

const (
	ModeBundled  Mode = "bundled"
	ModeExternal Mode = "external"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Installation struct {
	Mode      Mode        `json:"mode"`
	Endpoint  *url.URL    `json:"-"`
	Bootstrap Credentials `json:"bootstrap"`
	Migrator  Credentials `json:"migrator"`
	Admin     Credentials `json:"admin"`
	Runtime   Credentials `json:"runtime"`
}

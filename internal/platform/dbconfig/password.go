package dbconfig

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/url"

	platformmigrate "orbitjob/internal/platform/migrate"
)

const passwordBytes = 32

func NewBundled(endpointRaw string, owner Credentials, random io.Reader) (Installation, error) {
	endpoint, err := url.Parse(endpointRaw)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return Installation{}, fmt.Errorf("parse bundled database endpoint")
	}
	endpoint.User = nil
	passwords := make([]string, 3)
	for index := range passwords {
		passwords[index], err = generatePassword(random)
		if err != nil {
			return Installation{}, err
		}
	}
	return Installation{
		Mode:      ModeBundled,
		Endpoint:  endpoint,
		Bootstrap: owner,
		Migrator:  Credentials{Username: platformmigrate.RoleMigrator, Password: passwords[0]},
		Admin:     Credentials{Username: platformmigrate.RoleAdmin, Password: passwords[1]},
		Runtime:   Credentials{Username: platformmigrate.RoleRuntime, Password: passwords[2]},
	}, nil
}

func generatePassword(random io.Reader) (string, error) {
	buffer := make([]byte, passwordBytes)
	if _, err := io.ReadFull(random, buffer); err != nil {
		return "", fmt.Errorf("generate database password: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

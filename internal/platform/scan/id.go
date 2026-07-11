package scan

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// GenerateID returns a new ULID string suitable for CHAR(26) primary keys.
func GenerateID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

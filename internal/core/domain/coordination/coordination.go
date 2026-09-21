package coordination

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

func OccurrenceKey(sourceUID string, revisionID int64, scheduledAt time.Time) string {
	raw := fmt.Sprintf("%s:%d:%s", sourceUID, revisionID, scheduledAt.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

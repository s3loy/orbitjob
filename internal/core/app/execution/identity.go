package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// maxJobNameLength is the Kubernetes DNS-1123 subdomain limit for object names.
const maxJobNameLength = 63

// JobName derives the Kubernetes Job name for an attempt. The name is a pure
// function of run name and attempt number so that a controller restart computes
// the same name and adopts the existing Job instead of creating a second one.
// Long names are hashed to stay inside the DNS limit.
func JobName(identity Identity) string {
	raw := "oj-" + identity.RunName + "-" + strconv.Itoa(identity.Attempt)
	if len(raw) <= maxJobNameLength {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	suffix := "-" + strconv.Itoa(identity.Attempt)
	// Budget: "oj-" + name + "-" + 8-char hash + suffix.
	keep := maxJobNameLength - len("oj-") - 1 - 8 - len(suffix)
	return "oj-" + sanitize(identity.RunName, keep) + "-" + hex.EncodeToString(sum[:])[:8] + suffix
}

// sanitize trims a name to a length while keeping it a legal DNS label body.
func sanitize(name string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(name) <= limit {
		return strings.Trim(name, "-")
	}
	return strings.Trim(name[:limit], "-")
}

func itoa(v int) string { return strconv.Itoa(v) }

func atoi(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if v < 1 {
		return 0, fmt.Errorf("attempt must be positive, got %d", v)
	}
	return v, nil
}

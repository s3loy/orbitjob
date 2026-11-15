package definition

// AdmissionIntent is reconstructed from the API server's final object. Proof
// originates in the claimed server operation; clients cannot set it in Apply.
type AdmissionIntent struct {
	TenantID           string
	RequestID          string
	Kind               string
	Namespace          string
	Name               string
	UID                string
	OldUID             string
	OldResourceVersion string
	Digest             string
	Proof              string
}

const RequestAnnotation = "orbitjob.io/admin-request-id"
const DigestAnnotation = "orbitjob.io/admin-manifest-digest"
const ProofAnnotation = "orbitjob.io/admin-admission-proof"

package policy

// Record is one stored policy row.
type Record struct {
	ID          string
	TenantID    string // empty for a platform preset
	Name        string
	Description string
	Document    Document
}

// IsPlatformPreset reports whether this policy belongs to the installation
// rather than to a tenant. Presets are bindable by every tenant and editable by
// none.
func (r Record) IsPlatformPreset() bool { return r.TenantID == "" }

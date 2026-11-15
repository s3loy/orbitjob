package function

import "testing"

func TestNormalizeManagedDefinition(t *testing.T) {
	base := Definition{TenantID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: " charge ", Image: " app:v1 ", TimeoutSeconds: 60, Status: StatusActive}
	for _, tc := range []struct {
		name   string
		change func(*Definition)
	}{
		{"empty name", func(d *Definition) { d.Name = " " }},
		{"empty image", func(d *Definition) { d.Image = " " }},
		{"invalid status", func(d *Definition) { d.Status = "deleted" }},
		{"zero timeout", func(d *Definition) { d.TimeoutSeconds = 0 }},
		{"negative retention", func(d *Definition) { d.HistorySuccess = -1 }},
		{"invalid group", func(d *Definition) { d.ResourceGroupID = "foreign" }},
		{"null description", func(d *Definition) { d.Description = "text\x00" }},
		{"nested null label", func(d *Definition) { d.Labels = map[string]any{"nested": []string{"text\x00"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := base
			tc.change(&d)
			if _, err := NormalizeManagedDefinition(d); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	got, err := NormalizeManagedDefinition(base)
	if err != nil || got.Name != "charge" || got.Image != "app:v1" || got.Command == nil || got.Labels == nil {
		t.Fatalf("normalized = %+v, error = %v", got, err)
	}
}

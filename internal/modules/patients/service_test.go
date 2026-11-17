package patients

import (
	"testing"
	"time"
)

func TestBirthValidationUsesClinicDate(t *testing.T) {
	// At local New Year, UTC is still on the preceding date/year.
	now := time.Date(2026, 1, 1, 0, 30, 0, 0, time.FixedZone("clinic", 3*60*60))
	today := "2026-01-01"
	tomorrow := "2026-01-02"
	year := 2026
	for _, tc := range []struct {
		name  string
		d     Demographics
		valid bool
	}{
		{"today", Demographics{GivenName: "Synthetic", FamilyName: "Patient", BirthPrecision: "exact", BirthDate: &today}, true},
		{"tomorrow", Demographics{GivenName: "Synthetic", FamilyName: "Patient", BirthPrecision: "exact", BirthDate: &tomorrow}, false},
		{"current approximate year", Demographics{GivenName: "Synthetic", FamilyName: "Patient", BirthPrecision: "year", BirthYear: &year}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := clean(&tc.d, now)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, e)
			}
		})
	}
}

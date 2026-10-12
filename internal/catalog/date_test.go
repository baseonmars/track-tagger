package catalog

import "testing"

func TestValidateDate(t *testing.T) {
	for _, date := range []string{"0001", "9999", "1970", "1970-01", "2000-02-29", "2024-02-29", "0001-01-01"} {
		if err := ValidateDate(date); err != nil {
			t.Errorf("%q: %v", date, err)
		}
	}
	for _, date := range []string{"", "0000", "0000-01", "10000", "1970-1", "1970-00", "1970-13", "1970-01-00", "1970-04-31", "1900-02-29", "2023-02-29", " 1970", "1970 ", "1970/01", "unknown"} {
		if err := ValidateDate(date); err == nil {
			t.Errorf("accepted %q", date)
		}
	}
}

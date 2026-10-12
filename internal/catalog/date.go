package catalog

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var concertDate = regexp.MustCompile(`^[0-9]{4}(?:-[0-9]{2}(?:-[0-9]{2})?)?$`)

// ValidateDate accepts a concert date without inventing missing months or days.
func ValidateDate(date string) error {
	if !concertDate.MatchString(date) || strings.HasPrefix(date, "0000") {
		return errors.New("date must be YYYY, YYYY-MM, or YYYY-MM-DD (year 0001–9999)")
	}
	layout := map[int]string{4: "2006", 7: "2006-01", 10: "2006-01-02"}[len(date)]
	if _, err := time.Parse(layout, date); err != nil {
		return errors.New("date must be a valid YYYY, YYYY-MM, or YYYY-MM-DD")
	}
	return nil
}

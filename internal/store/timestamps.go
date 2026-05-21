package store

import (
	"time"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func validateCanonicalStoreTimestamp(recordName, pathKey, path, field, value string) error {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.UTC().Format(time.RFC3339) != value {
		return errors.NewWithDetails(
			errors.EStoreCorrupt,
			recordName+" has invalid "+field+" timestamp",
			map[string]string{
				pathKey: path,
				"field": field,
				"value": value,
			},
		)
	}
	return nil
}

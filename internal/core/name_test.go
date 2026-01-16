package core

import (
	"strings"
	"testing"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func TestValidateName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantCode  errors.Code
	}{
		// Valid names
		{"minimum length", "ab", false, ""},
		{"simple lowercase", "myfeature", false, ""},
		{"with hyphen", "my-feature", false, ""},
		{"with digits", "fix123", false, ""},
		{"digits after hyphen", "fix-bug-123", false, ""},
		{"multiple hyphens separated", "my-cool-feature", false, ""},
		{"alphanumeric mix", "a1b2c3", false, ""},
		{"max length 40", strings.Repeat("a", 40), false, ""},

		// Invalid: too short
		{"single char", "a", true, errors.EInvalidName},
		{"empty", "", true, errors.EInvalidName},

		// Invalid: too long
		{"41 chars", strings.Repeat("a", 41), true, errors.EInvalidName},
		{"50 chars", strings.Repeat("a", 50), true, errors.EInvalidName},

		// Invalid: uppercase
		{"uppercase start", "MyFeature", true, errors.EInvalidName},
		{"uppercase middle", "myFeature", true, errors.EInvalidName},
		{"all uppercase", "MYFEATURE", true, errors.EInvalidName},

		// Invalid: starts with digit
		{"starts with digit", "1abc", true, errors.EInvalidName},
		{"starts with digit and hyphen", "1-abc", true, errors.EInvalidName},

		// Invalid: starts with hyphen
		{"starts with hyphen", "-abc", true, errors.EInvalidName},

		// Invalid: consecutive hyphens
		{"consecutive hyphens", "my--feature", true, errors.EInvalidName},
		{"triple hyphens", "my---feature", true, errors.EInvalidName},

		// Invalid: trailing hyphen
		{"trailing hyphen", "myfeature-", true, errors.EInvalidName},
		{"trailing hyphen after word", "my-feature-", true, errors.EInvalidName},

		// Invalid: special characters
		{"underscore", "my_feature", true, errors.EInvalidName},
		{"space", "my feature", true, errors.EInvalidName},
		{"dot", "my.feature", true, errors.EInvalidName},
		{"slash", "my/feature", true, errors.EInvalidName},
		{"at sign", "my@feature", true, errors.EInvalidName},
		{"colon", "my:feature", true, errors.EInvalidName},

		// Edge cases
		{"two chars with hyphen not allowed", "a-", true, errors.EInvalidName},
		{"hyphen only segment", "a--b", true, errors.EInvalidName},
		{"digit only after start", "a1", false, ""},
		{"exactly max with hyphens", "a-" + strings.Repeat("b", 37), false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateName(%q) = nil, want error", tt.input)
					return
				}
				gotCode := errors.GetCode(err)
				if gotCode != tt.wantCode {
					t.Errorf("ValidateName(%q) error code = %q, want %q", tt.input, gotCode, tt.wantCode)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateName(%q) = %v, want nil", tt.input, err)
				}
			}
		})
	}
}

func TestValidateName_ErrorDetails(t *testing.T) {
	// Verify error details contain the invalid name
	err := ValidateName("Bad-Name")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}

	ae, ok := errors.AsAgencyError(err)
	if !ok {
		t.Fatal("expected AgencyError")
	}

	if ae.Details == nil {
		t.Fatal("expected error details")
	}

	if ae.Details["name"] != "Bad-Name" {
		t.Errorf("error details[name] = %q, want %q", ae.Details["name"], "Bad-Name")
	}
}

func TestNameConstants(t *testing.T) {
	// Verify constants are sensible
	if NameMinLen < 1 {
		t.Errorf("NameMinLen = %d, want >= 1", NameMinLen)
	}
	if NameMaxLen < NameMinLen {
		t.Errorf("NameMaxLen = %d, want >= NameMinLen (%d)", NameMaxLen, NameMinLen)
	}
	if NameMaxLen > 100 {
		t.Errorf("NameMaxLen = %d, want <= 100 (reasonable limit)", NameMaxLen)
	}
}

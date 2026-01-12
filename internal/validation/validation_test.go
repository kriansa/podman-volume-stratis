package validation

import (
	"testing"
)

func TestValidateVolumeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"valid simple name", "myvolume", false},
		{"valid with numbers", "volume123", false},
		{"valid with underscore", "my_volume", false},
		{"valid with dot", "my.volume", false},
		{"valid with hyphen", "my-volume", false},
		{"valid mixed", "my-volume_123.test", false},
		{"valid minimum length", "ab", false},
		{"valid 65 chars", "abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz123", false},
		{"valid starts with number", "1volume", false},

		// Invalid names - too short
		{"too short - 1 char", "a", true},
		{"too short - empty", "", true},

		// Invalid names - too long
		{"too long - 66 chars", "abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz1234", true},

		// Invalid names - bad characters
		{"starts with underscore", "_volume", true},
		{"starts with hyphen", "-volume", true},
		{"starts with dot", ".volume", true},
		{"contains space", "my volume", true},
		{"contains slash", "my/volume", true},
		{"contains colon", "my:volume", true},
		{"contains at sign", "my@volume", true},
		{"contains special chars", "my$volume", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateVolumeName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateVolumeName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

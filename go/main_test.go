package main

import "testing"

func TestValidateCityName(t *testing.T) {
	tests := []struct {
		city    string
		wantErr bool
	}{
		{"Munich", false},
		{"New York", false},
		{"", true},
		{"A", true},
		{"A" + string(make([]byte, 100)), true},
		{"123", false},
	}

	for _, tt := range tests {
		err := validateCityName(tt.city)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateCityName(%q) error = %v, wantErr %v", tt.city, err, tt.wantErr)
		}
	}
}

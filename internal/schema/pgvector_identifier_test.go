package schema

import "testing"

func TestSanitizePGIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "lowercase simple identifier", input: "items", want: "items"},
		{name: "normalizes mixed case and separators", input: "User-Profile.Vector", want: "user_profile_vector"},
		{name: "prefixes leading digit", input: "123items", want: "t_123items"},
		{name: "avoids reserved words", input: "select", want: "select_"},
		{name: "rejects empty identifier", input: "---", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SanitizePGIdentifier(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("identifier mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestUniquePGIdentifiersAvoidCollisions(t *testing.T) {
	got, err := UniquePGIdentifiers([]string{"User-ID", "user_id", "select", "select"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"User-ID": "user_id",
		"user_id": "user_id_2",
		"select":  "select__2",
	}
	for source, target := range want {
		if got[source] != target {
			t.Fatalf("mapping for %q: got %q want %q", source, got[source], target)
		}
	}
}

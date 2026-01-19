package modelutil

import "testing"

func TestValidateMediaResolution(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{in: "", want: "", wantOK: true},
		{in: " low ", want: "low", wantOK: true},
		{in: "MEDIUM", want: "medium", wantOK: true},
		{in: "high", want: "high", wantOK: true},
		{in: "MEDIA_RESOLUTION_LOW", want: "low", wantOK: true},
		{in: "media_resolution_medium", want: "medium", wantOK: true},
		{in: "Media_Resolution_High", want: "high", wantOK: true},
		{in: "ultra_high", want: "", wantOK: false},
		{in: "invalid", want: "", wantOK: false},
	}
	for _, tc := range cases {
		got, ok := ValidateMediaResolution(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("ValidateMediaResolution(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestToAPIMediaResolution(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{in: "", want: "", wantOK: true},
		{in: "low", want: "MEDIA_RESOLUTION_LOW", wantOK: true},
		{in: "MEDIUM", want: "MEDIA_RESOLUTION_MEDIUM", wantOK: true},
		{in: " high ", want: "MEDIA_RESOLUTION_HIGH", wantOK: true},
		{in: "MEDIA_RESOLUTION_HIGH", want: "MEDIA_RESOLUTION_HIGH", wantOK: true},
		{in: "ultra_high", want: "", wantOK: false},
	}
	for _, tc := range cases {
		got, ok := ToAPIMediaResolution(tc.in)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("ToAPIMediaResolution(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

package main

import "testing"

func TestFriendlyOPError(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		message string
		want    string
	}{
		{
			name:    "expired contextId",
			code:    "invalid_request",
			message: "Received+an+expired+contextId,+can+occur+if+the+login+took+too+long",
			want:    "the login took too long and the session expired, please try again",
		},
		{
			name:    "other message passed through with + decoded",
			code:    "some_error",
			message: "Something+went+wrong",
			want:    "Something went wrong",
		},
		{
			name:    "code only",
			code:    "bad_request",
			message: "",
			want:    "login failed (bad_request)",
		},
		{
			name:    "nothing",
			code:    "",
			message: "",
			want:    "login failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := friendlyOPError(tc.code, tc.message); got != tc.want {
				t.Errorf("friendlyOPError(%q, %q) = %q, want %q", tc.code, tc.message, got, tc.want)
			}
		})
	}
}

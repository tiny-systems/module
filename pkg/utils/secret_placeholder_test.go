package utils

import (
	"reflect"
	"testing"
)

func TestPlaceholderForAbsentSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want interface{}
	}{
		{
			// The screenshot case: an apiKey mapped through a generic boundary
			// resolves to null at sim time and fails `type: string`.
			name: "nested null apiKey gets a placeholder",
			in:   map[string]interface{}{"inputData": map[string]interface{}{"apiKey": nil, "logs": "line"}},
			want: map[string]interface{}{"inputData": map[string]interface{}{"apiKey": simulatedSecretPlaceholder, "logs": "line"}},
		},
		{
			name: "a real credential is left alone so genuine errors still surface",
			in:   map[string]interface{}{"apiKey": "sk-actual-value"},
			want: map[string]interface{}{"apiKey": "sk-actual-value"},
		},
		{
			name: "a non-credential null is untouched",
			in:   map[string]interface{}{"url": nil},
			want: map[string]interface{}{"url": nil},
		},
		{
			name: "matches the naming variants the redactor covers",
			in: map[string]interface{}{
				"api_key": nil, "authToken": nil, "password": nil,
				"authorization": nil, "accessKey": nil, "privateKey": nil,
			},
			want: map[string]interface{}{
				"api_key": simulatedSecretPlaceholder, "authToken": simulatedSecretPlaceholder,
				"password": simulatedSecretPlaceholder, "authorization": simulatedSecretPlaceholder,
				"accessKey": simulatedSecretPlaceholder, "privateKey": simulatedSecretPlaceholder,
			},
		},
		{
			name: "descends into arrays",
			in:   map[string]interface{}{"items": []interface{}{map[string]interface{}{"token": nil}}},
			want: map[string]interface{}{"items": []interface{}{map[string]interface{}{"token": simulatedSecretPlaceholder}}},
		},
		{
			name: "scalars pass through",
			in:   "plain",
			want: "plain",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := placeholderForAbsentSecrets(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("placeholderForAbsentSecrets() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// The placeholder must never look like a usable credential, in case it reaches
// a log or a message.
func TestPlaceholderIsNotCredentialShaped(t *testing.T) {
	if secretFieldRe.MatchString(simulatedSecretPlaceholder) {
		t.Errorf("placeholder %q reads as a credential", simulatedSecretPlaceholder)
	}
}

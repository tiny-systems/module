package secret
import "testing"
func TestContainsPlaceholder(t *testing.T) {
	cases := []struct{ name string; v any; want bool }{
		{"struct with placeholder", struct{ APIKey string }{"[[secret:my-keys/anthropic]]"}, true},
		{"struct no placeholder", struct{ APIKey string }{"sk-ant-real"}, false},
		{"map settings placeholder", map[string]any{"apiKey": "[[secret:demo/k]]", "model": "haiku"}, true},
		{"map settings none", map[string]any{"apiKey": "", "model": "haiku"}, false},
		{"nested map", map[string]any{"context": map[string]any{"dsn": "[[secret:db/dsn]]"}}, true},
		{"slice", []any{"x", "[[secret:a/b]]"}, true},
		{"substring is NOT a whole placeholder", map[string]any{"h": "Bearer [[secret:a/b]]"}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := ContainsPlaceholder(tc.v); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

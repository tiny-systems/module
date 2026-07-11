package utils
import ("encoding/json"; "testing")
func TestSpreadContext_MatchesRuntimeShape(t *testing.T) {
	// Sim-shaped router out_home: user context nested 3 deep, realIP only at
	// $.context.realIP — mirrors the myipaddress false-red case.
	raw := `{"conditions":[{"route":"HOME"}],"context":{"body":"x","realIP":"1.2.3.4","context":{"autoHostName":false,"context":{"maxmind_account":"1059591"}}}}`
	var v interface{}
	json.Unmarshal([]byte(raw), &v)
	out := spreadContext(v).(map[string]interface{})
	ctx := out["context"].(map[string]interface{})
	// auth expr $.context.maxmind_account must now resolve
	if ctx["maxmind_account"] != "1059591" {
		t.Errorf("$.context.maxmind_account not spread: %v", ctx["maxmind_account"])
	}
	// URL expr $.realIP must now resolve (spread from $.context.realIP to root)
	if out["realIP"] != "1.2.3.4" {
		t.Errorf("$.realIP not spread to root: %v", out["realIP"])
	}
	// nested context preserved (non-destructive)
	if _, ok := ctx["context"]; !ok {
		t.Errorf("nested context wrongly removed")
	}
}

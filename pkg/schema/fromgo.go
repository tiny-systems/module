package schema

import "encoding/json"

// FromGo builds a port schema from a Go value, ready to assign to
// module.Port.Schema. It is the bridge for components that have a Go type for
// the shape they want to publish but still need the bytes — e.g. picking one of
// several control shapes at runtime:
//
//	{Name: v1alpha1.ControlPort, Source: true, Schema: schema.FromGo(c.getControl())}
//
// A port whose shape IS its Go type needs none of this: leave Schema nil and
// the runtime reflects Configuration as before. Returns nil on error, which
// makes the runtime fall back to that reflection rather than publishing a
// broken schema.
func FromGo(val interface{}) json.RawMessage {
	s, err := CreateSchema(val)
	if err != nil {
		return nil
	}
	b, err := s.MarshalJSON()
	if err != nil {
		return nil
	}
	return b
}

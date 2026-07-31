package cli

// The shape a module reports for its components, used by `tools
// components-info` to introspect what this module offers and to run the
// error-port conformance check over it.
//
// These used to be the generated PublishComponent / PublishComponentPort types
// from the platform's OpenAPI spec, left over from when the SDK published
// modules to the platform over HTTP. That command is gone — a module is built
// by its own CI and discovered from a repo index — so the types described a
// request nobody sends, and the spec kept carrying them only because this
// package compiled against them. What a module says about its own components
// belongs to the module, so it lives here now, and the SDK no longer depends on
// the platform's API package at all.
//
// Nothing marshals these: they are built in memory and read by the checks in
// this package. The json tags match the shape consumers already knew, so
// serialising one stays compatible if that is ever wanted.

type componentShape struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Info        *string               `json:"info,omitempty"`
	Tags        *[]string             `json:"tags,omitempty"`
	Ports       *[]componentPortShape `json:"ports,omitempty"`
}

type componentPortShape struct {
	Name        string                  `json:"name"`
	Label       *string                 `json:"label,omitempty"`
	Source      bool                    `json:"source,omitempty"`
	Position    *int                    `json:"position,omitempty"`
	Schema      *map[string]interface{} `json:"schema,omitempty"`
	DefaultData *map[string]interface{} `json:"default_data,omitempty"`
}

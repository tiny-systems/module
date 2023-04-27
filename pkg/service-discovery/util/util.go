package util

import (
	"bytes"
	"github.com/goccy/go-json"
)

// Unmarshal parses the encoded data and stores the result
// in the value pointed to by v
func Unmarshal(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewBuffer(data))
	return dec.Decode(v)
}

// Marshal encodes v and returns encoded data
func Marshal(v interface{}) ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	err := enc.Encode(v)
	if err != nil {
		return []byte{}, err
	}
	return buf.Bytes(), nil
}

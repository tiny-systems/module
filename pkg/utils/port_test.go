package utils

import "testing"

func TestParseFullPortName(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name     string
		args     args
		wantNode string
		wantPort string
	}{
		{
			name: "normal",
			args: args{
				name: "node:port",
			},
			wantNode: "node",
			wantPort: "port",
		},
		{
			name: "onlyport",
			args: args{
				name: "port",
			},
			wantNode: "",
			wantPort: "port",
		},
		{
			name: "invalid1",
			args: args{
				name: "some:thing:off",
			},
			wantNode: "",
			wantPort: "",
		},
		{
			name: "empty",
			args: args{
				name: "",
			},
			wantNode: "",
			wantPort: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNode, gotPort := ParseFullPortName(tt.args.name)
			if gotNode != tt.wantNode {
				t.Errorf("ParseFullPortName() gotNode = %v, want %v", gotNode, tt.wantNode)
			}
			if gotPort != tt.wantPort {
				t.Errorf("ParseFullPortName() gotPort = %v, want %v", gotPort, tt.wantPort)
			}
		})
	}
}

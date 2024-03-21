package metrics

import (
	"fmt"
	"net"
	"net/url"
)

type DSN struct {
	original string

	Scheme   string
	Host     string
	GRPCPort string
	Token    string
}

func (dsn *DSN) String() string {
	return dsn.original
}

func (dsn *DSN) OTLPEndpoint() string {
	return joinHostPort(dsn.Host, dsn.GRPCPort)
}

func ParseDSN(dsnStr string) (*DSN, error) {
	if dsnStr == "" {
		return nil, fmt.Errorf("DSN is empty (use WithDSN or OTLP_DSN env var)")
	}

	u, err := url.Parse(dsnStr)
	if err != nil {
		return nil, fmt.Errorf("can't parse DSN=%q: %s", dsnStr, err)
	}

	if u.Scheme == "" {
		return nil, fmt.Errorf("DSN=%q does not have a scheme", dsnStr)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("DSN=%q does not have a host", dsnStr)
	}
	if u.User == nil {
		return nil, fmt.Errorf("DSN=%q does not have a token", dsnStr)
	}

	dsn := DSN{
		original: dsnStr,
		Scheme:   u.Scheme,
		Host:     u.Host,
		Token:    u.User.Username(),
	}

	if host, port, err := net.SplitHostPort(u.Host); err == nil {
		dsn.Host = host
		dsn.GRPCPort = port
	}

	if dsn.GRPCPort == "" {
		dsn.GRPCPort = "413"
	}
	return &dsn, nil
}

func joinHostPort(host, port string) string {
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

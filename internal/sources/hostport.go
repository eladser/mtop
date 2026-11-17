package sources

import "net/url"

// HostPort is the host:port of a server url, the key proxy traffic is
// matched to rows by. A bare host:port (no scheme) comes back as is.
func HostPort(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	if u, err := url.Parse("http://" + raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

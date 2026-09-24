package config

import "testing"

func TestLoadProxyAuth(t *testing.T) {
	cases := []struct {
		name    string
		cidrs   string
		header  string
		wantErr bool
		want    int
	}{
		{name: "disabled by default"},
		{name: "a single CIDR", cidrs: "10.0.0.0/8", header: "Remote-User", want: 1},
		{name: "comma and space separated", cidrs: "10.0.0.0/8, 192.168.0.0/16 ::1/128", header: "Remote-User", want: 3},
		{name: "every IPv4 client", cidrs: "0.0.0.0/0", header: "Remote-User", wantErr: true},
		{name: "every IPv6 client", cidrs: "::/0", header: "Remote-User", wantErr: true},
		{name: "not a CIDR", cidrs: "10.0.0.1", header: "Remote-User", wantErr: true},
		{name: "no user header", cidrs: "10.0.0.0/8", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("APP_AUTH_PROXY_TRUSTED_CIDRS", c.cidrs)
			t.Setenv("APP_AUTH_PROXY_USER_HEADER", c.header)

			proxy, err := loadProxyAuth()
			if c.wantErr != (err != nil) {
				t.Fatalf("got err %v, wanted an error: %v", err, c.wantErr)
			}
			if c.wantErr {
				return
			}
			if len(proxy.TrustedCIDRs) != c.want {
				t.Fatalf("got %d CIDRs, want %d", len(proxy.TrustedCIDRs), c.want)
			}
			if proxy.Enabled() != (c.want > 0) {
				t.Fatalf("Enabled() = %v with %d CIDRs", proxy.Enabled(), c.want)
			}
		})
	}
}

package fetcher

import (
	"net/netip"
	"testing"
)

func TestGetOtherP2PHost(t *testing.T) {
	tests := map[string]struct {
		IP           netip.Prefix
		wantedPrefix netip.Prefix
		wantError    bool
	}{
		"10.0.0.0/31": {
			wantedPrefix: netip.MustParsePrefix("10.0.0.1/31"),
		},
		"10.0.0.1/31": {
			wantedPrefix: netip.MustParsePrefix("10.0.0.0/31"),
		},
		"10.0.0.2/31": {
			wantedPrefix: netip.MustParsePrefix("10.0.0.3/31"),
		},
		"10.0.0.1/30": {
			wantedPrefix: netip.MustParsePrefix("10.0.0.2/30"),
		},
		"10.0.0.2/30": {
			wantedPrefix: netip.MustParsePrefix("10.0.0.1/30"),
		},
		"10.0.0.0/30": {
			wantedPrefix: netip.Prefix{},
			wantError:    true,
		},
		"10.0.0.3/30": {
			wantedPrefix: netip.Prefix{},
			wantError:    true,
		},
		"::/127": {
			wantedPrefix: netip.MustParsePrefix("::1/127"),
		},
		"::1/127": {
			wantedPrefix: netip.MustParsePrefix("::/127"),
		},
		"::1/64": {
			wantedPrefix: netip.Prefix{},
			wantError:    true,
		},
	}

	for cidr, test := range tests {
		prefix := netip.MustParsePrefix(cidr)
		other, err := GetOtherP2PHost(prefix)

		if (err != nil) != test.wantError {
			t.Fatalf("%s: error mismatch, want error: %t, got error: %t", cidr, (err != nil), test.wantError)
		}

		if other.String() != test.wantedPrefix.String() {
			t.Fatalf("%s: mismatch, want: %s, got: %s", cidr, test.wantedPrefix.String(), other.String())
		}
	}
}

package sonic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

func TestPSUStatus(t *testing.T) {
	for _, platform := range platforms {
		psus := []PSU{}
		fixtureJSON(t, platform, "psushow.json", &psus)

		if len(psus) != 2 {
			t.Fatalf("%s: want 2 power supplies, got %d", platform, len(psus))
		}

		for _, psu := range psus {
			// presence is reported as 'true' and status as 'OK' on both releases
			if psu.Presence.String() != "OK" || psu.Status.String() != "OK" {
				t.Errorf("%s: %s should be present and OK, got: %s / %s",
					platform, psu.Name, psu.Presence, psu.Status)
			}
			if psu.Model == "" || psu.Voltage.Value == nil || psu.Power.Value == nil {
				t.Errorf("%s: incomplete power supply: %+v", platform, psu)
			}
		}

		out, err := json.Marshal(psus)
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}
		if !strings.Contains(string(out), `"presence":true`) {
			t.Errorf("%s: presence should marshal as a boolean: %s", platform, out)
		}
	}
}

func TestNABool(t *testing.T) {
	tests := map[string]struct {
		value  string
		want   string
		isNull bool
	}{
		"true":       {"true", "OK", false},
		"True":       {"True", "OK", false},
		"ok":         {"OK", "OK", false},
		"false":      {"false", "not OK", false},
		"not ok":     {"Not OK", "not OK", false},
		"unknown":    {"N/A", "N/A", true},
		"no reading": {"", "N/A", true},
	}

	for name, test := range tests {
		value := NABool{}
		if err := value.UnmarshalText([]byte(test.value)); err != nil {
			t.Fatalf("%s: %s", name, err)
		}

		if value.String() != test.want {
			t.Errorf("%s: want: %s, got: %s", name, test.want, value)
		}

		out, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		if isNull := string(out) == "null"; isNull != test.isNull {
			t.Errorf("%s: marshalled as %s, want null: %t", name, out, test.isNull)
		}
	}
}

func TestNAFloat(t *testing.T) {
	tests := map[string]struct {
		value  string
		want   string
		isNull bool
	}{
		"measure":     {"3.29", "3.29", false},
		"padded":      {"  -8.14 ", "-8.14", false},
		"negative":    {"-40", "-40.00", false},
		"no light":    {"-inf", "", true},
		"infinite":    {"inf", "", true},
		"not a value": {"NaN", "", true},
		"unknown":     {"N/A", "", true},
		"no reading":  {"", "", true},
	}

	for name, test := range tests {
		value := NAFloat{}
		if err := value.UnmarshalText([]byte(test.value)); err != nil {
			t.Fatalf("%s: %s", name, err)
		}

		if value.String() != test.want {
			t.Errorf("%s: want: %q, got: %q", name, test.want, value)
		}

		out, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		if isNull := string(out) == "null"; isNull != test.isNull {
			t.Errorf("%s: marshalled as %s, want null: %t", name, out, test.isNull)
		}
	}
}

func TestImageVersion(t *testing.T) {
	tests := map[string]Version{
		"msn2700": {Build: "202211-R002.0-abc123def", Debian: "11.8", Kernel: "5.10.0-18-2-amd64", ASIC: "mellanox"},
		"w6510":   {Build: "202505-R001.1-def456abc", Debian: "12.13", Kernel: "6.1.0-29-2-amd64", ASIC: "broadcom"},
	}

	for platform, want := range tests {
		version := Version{}
		if err := yaml.Unmarshal([]byte(fixture(t, platform, "sonic_version.yml")), &version); err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		if version != want {
			t.Errorf("%s: wrong version,\nwant: %+v\ngot:  %+v", platform, want, version)
		}
	}
}

func TestSNMPConfig(t *testing.T) {
	for _, platform := range platforms {
		config := map[string]any{}
		if err := yaml.Unmarshal([]byte(fixture(t, platform, "snmp.yml")), &config); err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		for _, key := range []string{"snmp_rocommunity", "snmp_location", "snmp_contact"} {
			if config[key] == nil || config[key] == "" {
				t.Errorf("%s: %s is missing in %v", platform, key, config)
			}
		}
	}
}

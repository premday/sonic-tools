package fetcher

import (
	"slices"
	"testing"
)

func TestExtractOutputInterface(t *testing.T) {
	tests := map[string]struct {
		testFile string
		want     []Route
	}{
		"simple": {
			testFile: "./test_data/showiproute.txt",
			want: []Route{
				{"", "Ethernet224", true},
			},
		},
		"multiple": {
			testFile: "./test_data/showiproute_multiple.txt",
			want: []Route{
				{"10.1.0.225", "Ethernet0", false},
				{"10.1.0.227", "Ethernet8", false},
				{"10.1.0.229", "Ethernet64", false},
				{"10.1.0.231", "Ethernet72", false},
				{"10.1.0.233", "Ethernet128", false},
				{"10.1.0.235", "Ethernet136", false},
				{"10.1.0.237", "Ethernet192", false},
				{"10.1.0.239", "Ethernet200", false},
			},
		},
	}

	for name, test := range tests {
		var routeTable RouteTable
		loadData(t, test.testFile, &routeTable)
		outInterfaces := ExtractRoutes(routeTable)

		if !slices.Equal(outInterfaces, test.want) {
			t.Fatalf("%s: wrong out interfaces, want: %v, got: %v", name, test.want, outInterfaces)
		}
	}
}

package fetcher

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

func loadData(t *testing.T, testFile string, out any) {
	t.Helper()
	fd, err := os.Open(testFile)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()

	content, err := io.ReadAll(fd)
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(content, &out); err != nil {
		t.Fatal(err)
	}
}

func TestGetInterfaceNeighbor(t *testing.T) {
	tests := map[string]struct {
		testFile             string
		intf                 string
		wantedRemoteHostname string
		wantedRemotePort     string
	}{
		"simple": {
			testFile:             "./test_data/lldp.json",
			intf:                 "Ethernet224",
			wantedRemoteHostname: "leaf21.pod03.dc1.prod.example.net",
			wantedRemotePort:     "Ethernet64",
		},
		"full_to_mgmt": {
			testFile:             "./test_data/lldp_full.json",
			intf:                 "eth0",
			wantedRemoteHostname: "mgmt-sw01.pod01.dc2.mgmt.example.net",
			wantedRemotePort:     "Ethernet0",
		},
		"full_to_spine": {
			testFile:             "./test_data/lldp_full.json",
			intf:                 "Ethernet32",
			wantedRemoteHostname: "spine02.pod01.dc2.prod.example.net",
			wantedRemotePort:     "Ethernet96",
		},
		"full_to_server_inventory": {
			testFile:             "./test_data/lldp_full.json",
			intf:                 "Ethernet8",
			wantedRemoteHostname: "02:00:00:00:02:05",
			wantedRemotePort:     "02:00:00:00:02:05",
		},
		"full_to_server": {
			testFile:             "./test_data/lldp_full.json",
			intf:                 "Ethernet19",
			wantedRemoteHostname: "server0821.example.prod",
			wantedRemotePort:     "eth1",
		},
		"lab": {
			testFile:             "./test_data/lldp_lab.json",
			intf:                 "Ethernet8",
			wantedRemoteHostname: "server01.example.preprod",
			wantedRemotePort:     "eth1",
		},
	}

	for name, test := range tests {
		var lldp LLDP
		loadData(t, test.testFile, &lldp)
		remoteHost, remotePort := lldp.ExtractInterfaceNeighbor(test.intf)

		if remoteHost != test.wantedRemoteHostname {
			t.Fatalf("%s: wrong out hostname, want: %s, got: %s", name, test.wantedRemoteHostname, remoteHost)
		}

		if remotePort != test.wantedRemotePort {
			t.Fatalf("%s: wrong out interfaces, want: %s, got: %s", name, test.wantedRemotePort, remotePort)
		}
	}
}

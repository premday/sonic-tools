package fetcher

import (
	"net"
)

func isMACAddress(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

func FetchLLDPNeighbor() (LLDP, error) {
	var lldp LLDP
	if err := runCommand("lldpctl -f json", &lldp); err != nil {
		return LLDP{}, err
	}
	return lldp, nil
}

// ExtractInterfaceNeighbor returns the remote hostname and remote port for a given local interface.
func (l LLDP) ExtractInterfaceNeighbor(intf string) (string, string) {
	remoteHost, remoteIntf := "", ""
	for _, interfaces := range l.LLDP.Interface {
		for name, info := range interfaces {
			if name != intf {
				continue
			}

			for hostname := range info.Chassis {
				remoteHost = hostname
			}

			if isMACAddress(info.Port.ID.Value) && info.Port.Descr != "" {
				remoteIntf = info.Port.Descr
			} else {
				remoteIntf = info.Port.ID.Value
			}
		}
	}

	return remoteHost, remoteIntf
}

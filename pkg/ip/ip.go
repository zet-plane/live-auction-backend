package ip

import (
	"net"

	"github.com/zet-plane/live-auction-backend/pkg/colorful"
)

func GetLocalHost() (res []string) {
	netInterfaces, err := net.Interfaces()
	if err != nil {
		println(colorful.Red("net.Interfaces failed, err: " + err.Error()))
		return res
	}

	for i := 0; i < len(netInterfaces); i++ {
		if (netInterfaces[i].Flags & net.FlagUp) == 0 {
			continue
		}
		addrs, _ := netInterfaces[i].Addrs()
		for _, address := range addrs {
			if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					res = append(res, ipnet.IP.String())
				}
			}
		}
	}
	return res
}

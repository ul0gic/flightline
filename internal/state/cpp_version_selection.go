package state

import "github.com/ul0gic/flightline/internal/asc"

func latestCPPVersionID(versions []asc.Resource[asc.AppCustomProductPageVersionAttributes]) (string, error) {
	return asc.SelectCPPVersionID(versions)
}

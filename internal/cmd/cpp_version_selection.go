package cmd

import "github.com/ul0gic/flightline/internal/asc"

func highestCPPVersionID(versions []CustomProductPageVersionView) (string, error) {
	resources := make([]asc.Resource[asc.AppCustomProductPageVersionAttributes], 0, len(versions))
	for _, version := range versions {
		resources = append(resources, asc.Resource[asc.AppCustomProductPageVersionAttributes]{ID: version.ID, Type: version.Type, Attributes: version.Attributes})
	}
	return asc.SelectCPPVersionID(resources)
}

package asc

import (
	"errors"
	"fmt"
	"strings"
)

// SelectCPPVersionID selects the unique editable draft or latest comparable decimal version.
func SelectCPPVersionID(versions []Resource[AppCustomProductPageVersionAttributes]) (string, error) {
	if len(versions) == 0 {
		return "", errors.New("CPP has no versions")
	}
	var editable string
	for _, version := range versions {
		if version.ID == "" {
			return "", errors.New("CPP version has no ID")
		}
		if version.Attributes.State != "PREPARE_FOR_SUBMISSION" && version.Attributes.State != "REJECTED" {
			continue
		}
		if editable != "" {
			return "", errors.New("CPP has multiple editable versions; cannot select a unique target")
		}
		editable = version.ID
	}
	if editable != "" {
		return editable, nil
	}
	current := versions[0]
	for _, version := range versions[1:] {
		order, err := compareCPPVersion(version.Attributes.Version, current.Attributes.Version)
		if err != nil {
			return "", err
		}
		if order == 0 {
			return "", errors.New("CPP has ambiguous equal version numbers")
		}
		if order > 0 {
			current = version
		}
	}
	return current.ID, nil
}

func compareCPPVersion(a, b string) (int, error) {
	left, err := decimalCPPVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := decimalCPPVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range max(len(left), len(right)) {
		l, r := "0", "0"
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if len(l) < len(r) {
			return -1, nil
		}
		if len(l) > len(r) {
			return 1, nil
		}
		if order := strings.Compare(l, r); order != 0 {
			return order, nil
		}
	}
	return 0, nil
}

func decimalCPPVersion(version string) ([]string, error) {
	parts := strings.Split(version, ".")
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("cannot order CPP version %q", version)
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return nil, fmt.Errorf("cannot order CPP version %q", version)
			}
		}
		parts[i] = strings.TrimLeft(part, "0")
		if parts[i] == "" {
			parts[i] = "0"
		}
	}
	return parts, nil
}

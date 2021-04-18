package utils

import (
	"crypto/sha256"
	"fmt"
)

// RemoveString removes a string from the given slice
func RemoveString(slice *[]string, s string) (removed bool) {
	filtered := make([]string, 0, len(*slice))
	for _, item := range *slice {
		if item == s {
			removed = true
		} else {
			filtered = append(filtered, item)
		}
	}
	*slice = filtered
	return removed
}

// AddString adds a string to the given slice
func AddString(slice *[]string, s string) bool {
	for _, item := range *slice {
		if item == s {
			return false
		}
	}
	*slice = append(*slice, s)
	return true
}

// HasString returns true if the string exists within the given slice
func HasString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func ResourceName(ownerName, suffix string) string {
	maxLen := 63 - 8 - 1 - len(suffix)
	name := fmt.Sprintf("%s-%s", ownerName, suffix)
	if len(ownerName) > maxLen {
		h := sha256.New()
		_, _ = h.Write([]byte(name))
		nameDigest := fmt.Sprintf("%x", h.Sum(nil))[:7]
		name = fmt.Sprintf("%s-%s", ownerName[:maxLen], nameDigest)
	}
	return name
}

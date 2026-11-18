package update

import (
	"strconv"
	"strings"
)

type Version [3]uint64

func ParseVersion(s string) (Version, bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var v Version
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return Version{}, false
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return Version{}, false
		}
		v[i] = n
	}
	return v, true
}

func (v Version) Newer(o Version) bool {
	for i := range v {
		if v[i] != o[i] {
			return v[i] > o[i]
		}
	}
	return false
}

func parseCurrent(s string) (v Version, prerelease, ok bool) {
	core, suffix, hasSuffix := strings.Cut(s, "-")
	if hasSuffix && (suffix == "" || strings.Trim(suffix, "0123456789.abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") != "") {
		return Version{}, false, false
	}
	v, ok = ParseVersion(core)
	return v, hasSuffix, ok
}

package ghttp

import (
	"fmt"
	"net/http"
	"strings"
)

type HTTPVersion struct {
	Major    int
	Minor    int
	Raw      string
	Standard bool
}

var (
	Version09 = HTTPVersion{Major: 0, Minor: 9, Raw: "HTTP/0.9", Standard: true}

	Version10 = HTTPVersion{Major: 1, Minor: 0, Raw: "HTTP/1.0", Standard: true}

	Version11 = HTTPVersion{Major: 1, Minor: 1, Raw: "HTTP/1.1", Standard: true}

	Version20 = HTTPVersion{Major: 2, Minor: 0, Raw: "HTTP/2.0", Standard: true}

	Version30 = HTTPVersion{Major: 3, Minor: 0, Raw: "HTTP/3.0", Standard: true}

	Version2 = HTTPVersion{Major: 2, Minor: 0, Raw: "HTTP/2", Standard: false}
	Version3 = HTTPVersion{Major: 3, Minor: 0, Raw: "HTTP/3", Standard: false}
)

var StandardVersions = map[string]HTTPVersion{
	"HTTP/0.9": Version09,
	"HTTP/1.0": Version10,
	"HTTP/1.1": Version11,
	"HTTP/2.0": Version20,
	"HTTP/3.0": Version30,
}

var AllKnownVersions = map[string]HTTPVersion{
	"HTTP/0.9": Version09,
	"HTTP/1.0": Version10,
	"HTTP/1.1": Version11,
	"HTTP/2.0": Version20,
	"HTTP/2":   Version2,
	"HTTP/3.0": Version30,
	"HTTP/3":   Version3,
}

func GetStandardVersions() []HTTPVersion {
	return []HTTPVersion{
		Version09,
		Version10,
		Version11,
		Version20,
		Version30,
	}
}

func GetAllKnownVersions() []HTTPVersion {
	return []HTTPVersion{
		Version09,
		Version10,
		Version11,
		Version20,
		Version2,
		Version30,
		Version3,
	}
}

func FromString(versionStr string) (HTTPVersion, bool) {
	versionStr = strings.TrimSpace(versionStr)

	if v, exists := AllKnownVersions[versionStr]; exists {
		return v, true
	}

	major, minor, ok := http.ParseHTTPVersion(versionStr)
	if !ok {
		return HTTPVersion{}, false
	}

	standardVersion := fmt.Sprintf("HTTP/%d.%d", major, minor)
	if v, exists := StandardVersions[standardVersion]; exists {
		return v, true
	}

	return HTTPVersion{
		Major:    major,
		Minor:    minor,
		Raw:      versionStr,
		Standard: false,
	}, true
}

func (v HTTPVersion) Compare(other HTTPVersion) int {
	if v.Major != other.Major {
		return v.Major - other.Major
	}
	return v.Minor - other.Minor
}

func (v HTTPVersion) Equal(other HTTPVersion) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}

func (v HTTPVersion) GreaterThan(other HTTPVersion) bool {
	return v.Compare(other) > 0
}

func (v HTTPVersion) LessThan(other HTTPVersion) bool {
	return v.Compare(other) < 0
}

func (v HTTPVersion) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	return fmt.Sprintf("HTTP/%d.%d", v.Major, v.Minor)
}

func (v HTTPVersion) SupportsKeepAlive() bool {
	return v.GreaterThan(Version10) || v.Equal(Version11)
}

func (v HTTPVersion) SupportsPipelining() bool {
	return v.GreaterThan(Version10) || v.Equal(Version11)
}

func (v HTTPVersion) IsHTTP2OrHigher() bool {
	return v.Major >= 2
}

func (v HTTPVersion) IsHTTP3OrHigher() bool {
	return v.Major >= 3
}

//go:build !linux && !windows

package link

import (
	"fmt"
)

func listLinks() ([]Link, error) {
	return nil, fmt.Errorf("link: platform not supported")
}

package rawcap

import "fmt"

func openLive(interfaceName string, cfg Config) (Handle, error) {
	return nil, fmt.Errorf("rawcap: live capture not implemented on windows")
}

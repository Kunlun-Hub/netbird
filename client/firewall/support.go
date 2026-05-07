package firewall

import "fmt"

func validateNonLinuxUserspaceSupport(goos string, isUserspaceBind bool) error {
	if isUserspaceBind {
		return nil
	}

	return fmt.Errorf(
		"non-linux platform %s requires userspace WireGuard mode for firewall-backed traffic logging",
		goos,
	)
}

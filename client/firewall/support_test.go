package firewall

import "testing"

func TestValidateNonLinuxUserspaceSupport(t *testing.T) {
	t.Run("windows userspace bind is supported", func(t *testing.T) {
		if err := validateNonLinuxUserspaceSupport("windows", true); err != nil {
			t.Fatalf("expected windows userspace bind to be supported, got %v", err)
		}
	})

	t.Run("windows kernel mode is rejected", func(t *testing.T) {
		err := validateNonLinuxUserspaceSupport("windows", false)
		if err == nil {
			t.Fatal("expected error for non-userspace windows mode")
		}
		if got := err.Error(); got == "" || got == "windows" {
			t.Fatalf("expected descriptive error, got %q", got)
		}
	})

	t.Run("darwin kernel mode is rejected", func(t *testing.T) {
		err := validateNonLinuxUserspaceSupport("darwin", false)
		if err == nil {
			t.Fatal("expected error for non-userspace darwin mode")
		}
	})
}

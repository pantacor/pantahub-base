package models

import "testing"

func TestNeedsVerification(t *testing.T) {
	cases := []struct {
		name string
		ov   *OVModeExtension
		want bool
	}{
		{"nil", nil, false},
		{"default mode", &OVModeExtension{Mode: DefaultVerification, Status: ValidationNotNeeded}, false},
		{"claim mode pending", &OVModeExtension{Mode: ClaimVerification, Status: Pending}, false},
		{"tls pending", &OVModeExtension{Mode: TLSVerification, Status: Pending}, true},
		{"tls completed", &OVModeExtension{Mode: TLSVerification, Status: Completed}, false},
		{"manual pending", &OVModeExtension{Mode: ManualVerification, Status: Pending}, true},
		{"manual unknown status", &OVModeExtension{Mode: ManualVerification}, true},
		{"manual completed", &OVModeExtension{Mode: ManualVerification, Status: Completed}, false},
		{"manual not needed", &OVModeExtension{Mode: ManualVerification, Status: ValidationNotNeeded}, false},
	}
	for _, c := range cases {
		if got := c.ov.NeedsVerification(); got != c.want {
			t.Errorf("%s: NeedsVerification() = %v, want %v", c.name, got, c.want)
		}
	}
}

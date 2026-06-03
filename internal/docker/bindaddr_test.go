package docker

import "testing"

// TestPortConfigBindAddress — a port's HostIP controls which host interface the
// published port binds to. Empty defaults to 0.0.0.0 (all interfaces); a set
// value (e.g. 127.0.0.1) restricts exposure.
func TestPortConfigBindAddress(t *testing.T) {
	_, binds := portConfig([]PortMapping{{ContainerPort: 5432, HostIP: "127.0.0.1"}})
	pb := binds["5432/tcp"]
	if len(pb) != 1 || pb[0].HostIP != "127.0.0.1" {
		t.Fatalf("HostIP = %+v, want 127.0.0.1", pb)
	}

	_, binds = portConfig([]PortMapping{{ContainerPort: 5432}})
	pb = binds["5432/tcp"]
	if len(pb) != 1 || pb[0].HostIP != "0.0.0.0" {
		t.Errorf("default HostIP = %+v, want 0.0.0.0", pb)
	}
}

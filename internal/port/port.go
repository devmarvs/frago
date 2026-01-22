package port

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// IsPortFree checks if a specific port is available.
func IsPortFree(port int) bool {
	checks := []struct {
		network        string
		addr           string
		skipOnNoFamily bool
	}{
		{"tcp4", fmt.Sprintf("0.0.0.0:%d", port), false},
		{"tcp4", fmt.Sprintf("127.0.0.1:%d", port), false},
		{"tcp6", fmt.Sprintf("[::]:%d", port), true},
		{"tcp6", fmt.Sprintf("[::1]:%d", port), true},
	}

	for _, check := range checks {
		ok, err := canListen(check.network, check.addr)
		if ok {
			continue
		}
		if check.skipOnNoFamily && err != nil && isAddrFamilyUnsupported(err) {
			continue
		}
		return false
	}
	return true
}

func canListen(network, addr string) (bool, error) {
	ln, err := net.Listen(network, addr)
	if err != nil {
		return false, err
	}
	_ = ln.Close()
	return true, nil
}

func isAddrFamilyUnsupported(err error) bool {
	return errors.Is(err, syscall.EAFNOSUPPORT) ||
		errors.Is(err, syscall.EPROTONOSUPPORT) ||
		errors.Is(err, syscall.EADDRNOTAVAIL)
}

// FindFreePort finds an available TCP port in the given range.
func FindFreePort(start, end int) (int, error) {
	for p := start; p <= end; p++ {
		if IsPortFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in range %d-%d", start, end)
}

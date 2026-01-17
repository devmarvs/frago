package port

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// IsPortFree checks if a specific port is available.
func IsPortFree(port int) bool {
	addr4 := fmt.Sprintf("0.0.0.0:%d", port)
	ok, err := canListen("tcp4", addr4)
	if !ok {
		return false
	}

	addr6 := fmt.Sprintf("[::]:%d", port)
	ok, err = canListen("tcp6", addr6)
	if ok {
		return true
	}
	if err != nil && isAddrFamilyUnsupported(err) {
		return true
	}
	return false
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

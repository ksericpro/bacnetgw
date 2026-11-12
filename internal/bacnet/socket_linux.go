//go:build linux

package bacnet

import "syscall"

const linuxSOReusePort = 15

func socketControl(reusePort bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			if reusePort {
				sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, linuxSOReusePort, 1)
			}
		})
		if err != nil {
			return err
		}
		return sockErr
	}
}


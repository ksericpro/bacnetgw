//go:build windows

package bacnet

import "syscall"

func socketControl(reusePort bool) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var sockErr error
		err := c.Control(func(fd uintptr) {
			handle := syscall.Handle(fd)
			_ = syscall.SetsockoptInt(handle, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			if reusePort {
				sockErr = syscall.SetsockoptInt(handle, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			}
		})
		if err != nil {
			return err
		}
		return sockErr
	}
}


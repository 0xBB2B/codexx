package proxy

import (
	"errors"
	"syscall"
)

// Windows 的 net.Listen 返回 WSAEADDRINUSE(10048)，而 syscall.EADDRINUSE 是 Go 自造的另一个值。
const wsaEADDRINUSE = syscall.Errno(10048)

func isAddrInUse(err error) bool {
	return errors.Is(err, wsaEADDRINUSE)
}

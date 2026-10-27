//go:build windows

package engine

// lockFile Windows 下退化为不加锁（服务端只用 Linux）。
func lockFile(path string) (func(), error) { return func() {}, nil }

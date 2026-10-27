//go:build !windows

package engine

import (
	"os"
	"syscall"
)

// lockFile 对路径加独占排它锁（非阻塞）。返回解锁函数。
// 用于防止 cron 与 agent 同时跑 quota check 导致重复累计。
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// acquireLockWithHandoff は多重起動を防ぐためのファイルロックを取得する。
// 既にロックが取られている場合(=cronプロセスが稼働中)、PIDファイルからそのプロセスを
// TERM→(5秒待機)→KILLの順で停止させてからロックを取得し直す。
// Ruby版 main() 冒頭のロック処理に対応。
func acquireLockWithHandoff(lockPath, pidPath string) (*os.File, error) {
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("ロックファイルを開けません: %w", err)
	}

	err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return lockFile, nil // 即座に取得できた(通常ケース)
	}

	fmt.Println("\x1b[33m[Notice] バックグラウンドで転送プロセス(Cron)が稼働中です。\x1b[0m")

	if pid, ok := readPID(pidPath); ok && pid > 0 {
		fmt.Printf("プロセス(PID: %d)を停止し、処理を引き継ぎます...", pid)

		_ = syscall.Kill(pid, syscall.SIGTERM)

		for i := 0; i < 5; i++ {
			time.Sleep(1 * time.Second)
			if !processAlive(pid) {
				break
			}
			fmt.Print(".")
		}

		if processAlive(pid) {
			fmt.Print(" 応答がないため強制終了します(KILL)...")
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	fmt.Print(" ロック取得...")
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("ロック取得に失敗しました: %w", err)
	}
	fmt.Println(" 完了。\n\x1b[32m>> 処理を引き継いで起動します。\x1b[0m")
	time.Sleep(1 * time.Second)

	return lockFile, nil
}

func readPID(pidPath string) (int, bool) {
	body, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

// processAlive はシグナル0を送ってプロセスの生存を確認する(実際にはシグナルを送らない)。
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM: 権限は無いが存在はする(生存とみなす)。ESRCH: 存在しない。
	return err == syscall.EPERM
}

func writePID(pidPath string) error {
	return os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func removePID(pidPath string) {
	_ = os.Remove(pidPath)
}

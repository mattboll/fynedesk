// Command tyde_runner is a crash recovery wrapper that manages Tyde sessions and restarts on failure.
package main

import (
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const runCmd = "tyde"

// exitNoX is the status tyde exits with, under the runner, when it cannot
// reach the X server: the session is over, it is not restarted. (512 was
// used before, which an exit status, 8 bits, cannot carry.)
const exitNoX = 3

// Restarts after a crash wait, longer each time tyde crashes soon after it
// started, so a crash at start does not spin.
const (
	restartPause    = time.Second
	restartPauseMax = 30 * time.Second
	stableRun       = time.Minute // a run this long resets the pause
)

func main() {
	_ = os.Remove(logPath()) // remove old logs
	_ = os.Remove(runnerLogPath())
	log.SetOutput(openRunnerLogWriter())
	launchEnv := os.Environ()
	pause := restartPause

	for {
		logFile := logPath()
		if _, err := os.Stat(logFile); err == nil {
			crashFile := crashLogPath()
			err = os.Rename(logFile, crashFile)
			if err != nil {
				log.Println("Could not save crash file", crashFile)
			}
		}

		exe := exec.Command(runCmd)
		exe.Env = append(launchEnv, "FYNE_DESK_RUNNER=1")
		logger := openLogWriter()
		exe.Stdout, exe.Stderr = logger, logger
		started := time.Now()
		err := exe.Run()
		_ = logger.Close() // on every path out of this run
		if err == nil {
			return
		}

		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			log.Println("Could not execute", runCmd, "command")
			return
		}

		if exit, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			switch status := exit.ExitStatus(); status {
			case 0:
				log.Println("Exiting Error 0")
				return
			case exitNoX:
				log.Println("X server went away")
				return
			default:
				log.Println("Restart from status", status)
			}
		}

		if time.Since(started) > stableRun {
			pause = restartPause
		}
		time.Sleep(pause)
		pause = min(pause*2, restartPauseMax)
	}
}

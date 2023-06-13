package wait

import (
	"golang.org/x/sys/unix"
)

func ForPID(pid int) error {
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return err
	}

	pollfd := unix.PollFd{
		Fd:      int32(pidfd),
		Events:  unix.POLLIN,
		Revents: 0,
	}
	_, err = unix.Poll([]unix.PollFd{pollfd}, -1)
	return err
}

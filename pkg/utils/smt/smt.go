package smt

import (
	"io"
	"os"
)

func Active() (bool, error) {
	f, err := os.Open("/sys/devices/system/cpu/smt/active")

	if err != nil {
		return false, err
	}
	defer f.Close()

	var buf [1]byte
	_, err = io.ReadFull(f, buf[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return false, err
	}

	return buf[0] == '1', nil
}

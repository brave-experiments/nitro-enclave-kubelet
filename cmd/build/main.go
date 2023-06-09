package main

import (
	"fmt"
	"log"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/build"
	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/cli"
)

func main() {
	err := build.BuildEif("/usr/share/nitro_enclaves/blobs/", "busybox", []string{"/bin/sh", "-c", "watch echo $FOO"}, map[string]string{"FOO": "hello world"}, "hello.eif")
	if err != nil {
		log.Fatal(err)
	}

	info, err := cli.RunEnclave(&cli.EnclaveConfig{
		CPUCount:  2,
		MemoryMib: 128,
		EifPath:   "hello.eif",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(info)
}

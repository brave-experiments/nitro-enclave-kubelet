package main

import (
	"fmt"
	"log"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/build"
	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/cli"
)

func main() {
	err := build.BuildEif("/usr/share/nitro_enclaves/blobs/", "busybox", []string{"/bin/sh", "-c", "echo $FOO"}, map[string]string{"FOO": "hello world"}, "hello.eif")
	if err != nil {
		log.Fatal(err)
	}

	info, err := cli.RunEnclave(&cli.EnclaveConfig{
		CPUCount:  1,
		MemoryMib: 100,
		EifPath:   "hello.eif",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(info)
}

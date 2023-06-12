package main

import (
	"log"
	"os"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/build"
)

func main() {
	file, err := os.CreateTemp("", "bootstrap")
	if err != nil {
		log.Fatal(err)
	}

	err = build.BuildEif("/usr/share/nitro_enclaves/blobs/", "busybox", []string{"/bin/sh", "-c", "watch echo $FOO"}, map[string]string{"FOO": "hello world"}, file.Name())
	if err != nil {
		log.Fatal(err)
	}
}

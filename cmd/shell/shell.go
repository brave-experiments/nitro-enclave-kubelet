package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/utils/nitro"
	appctx "github.com/brave-intl/bat-go/libs/context"

	"github.com/mdlayher/vsock"
	"github.com/rs/zerolog"
)

type RemoteWriter struct {
	LocalWriter  io.Writer
	RemoteWriter io.Writer
}

func (w *RemoteWriter) Write(p []byte) (n int, err error) {
	n, err = w.LocalWriter.Write(p)
	if err != nil {
		return n, err
	}
	return w.RemoteWriter.Write(p)
}

func Listen(p uint) {
	ctx := context.Background()
	cid, err := vsock.ContextID()
	if err == nil {
		writer := RemoteWriter{
			RemoteWriter: nitro.NewVsockWriter(fmt.Sprintf("vm(4):%d", 10000+cid)),
			LocalWriter:  zerolog.ConsoleWriter{Out: os.Stderr},
		}
		ctx = zerolog.New(&writer).WithContext(ctx)
	}

	logger, err := appctx.GetLogger(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}

	l, err := vsock.Listen(uint32(p), &vsock.Config{})
	if nil != err {
		log.Fatalf("Could not bind to interface: %v", err)
	}
	defer l.Close()
	logger.Info().Str("addr", l.Addr().String()).Str("network", l.Addr().Network()).Msg("Listening on")
	for {
		c, err := l.Accept()
		if nil != err {
			log.Fatalf("Could not accept connection: %v", err)
		}
		logger.Info().Str("addr", c.RemoteAddr().String()).Msg("Accepted connection")

		cmd := exec.Command("/bin/bash", "-i")
		cmd.Stdin = c
		cmd.Stdout = c
		cmd.Stderr = c
		cmd.Run()
	}
}

func Connect(i *string, p uint) {
	sock := fmt.Sprintf("%s:%d", *i, p)
	c, err := net.Dial("tcp", sock)
	if nil != err {
		log.Fatalf("Could not open TCP connection: %v", err)
	}
	defer c.Close()
	log.Println("TCP connection established")

	go io.Copy(c, os.Stdin)
	go io.Copy(os.Stdout, c)
	for {
	}
}

func main() {
	p := flag.Uint("p", 4444, "Port")
	l := flag.Bool("l", false, "Listen")
	c := flag.String("c", "", "Connect IP")
	flag.Parse()
	if *l {
		Listen(*p)
	} else {
		Connect(c, *p)
	}
}

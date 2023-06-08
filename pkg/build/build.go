package build

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const (
	bootstrapTemplate = `files:
  - path: dev
    directory: true
    mode: "0755"
  - path: init
    source: {{ .initPath }}
    mode: "0755"
  - path: nsm.ko
    source: {{ .nsmkoPath }}
    mode: "0755"`
	customerTemplate = `init:
  - {{ .image }}
files:
  - path: rootfs/dev
    directory: true
    mode: "0755"
  - path: rootfs/run
    directory: true
    mode: "0755"
  - path: rootfs/sys
    directory: true
    mode: "0755"
  - path: rootfs/var
    directory: true
    mode: "0755"
  - path: rootfs/proc
    directory: true
    mode: "0755"
  - path: rootfs/tmp
    directory: true
    mode: "0755"
  - path: cmd
    source: {{ .cmd }}
    mode: "0644"
  - path: env
    source: {{ .env }}
    mode: "0644"`
)

func generateBootstrap(initPath, nsmkoPath string) (*os.File, error) {
	file, err := os.CreateTemp("", "bootstrap")
	if err != nil {
		return nil, err
	}
	templ := template.Must(template.New("bootstrap").Parse(bootstrapTemplate))
	err = templ.Execute(file, map[string]interface{}{
		"initPath":  initPath,
		"nsmkoPath": nsmkoPath,
	})
	return file, err
}

func generateCustomer(image, cmdPath, envPath string) (*os.File, error) {
	file, err := os.CreateTemp("", "customer")
	if err != nil {
		return nil, err
	}
	templ := template.Must(template.New("customer").Parse(customerTemplate))
	err = templ.Execute(file, map[string]interface{}{
		"image": image,
		"cmd":   cmdPath,
		"env":   envPath,
	})
	return file, err
}

func BuildEif(blobsPath string, image string, cmds []string, envs map[string]string, output string) error {
	artifactsDir, err := os.MkdirTemp("", "initramfs")
	if err != nil {
		return err
	}
	defer os.RemoveAll(artifactsDir)

	bootstrap, err := generateBootstrap(filepath.Join(blobsPath, "init"), filepath.Join(blobsPath, "nsm.ko"))
	if err != nil {
		return err
	}
	defer os.Remove(bootstrap.Name())

	cmd, err := os.CreateTemp("", "cmd")
	if err != nil {
		return err
	}
	defer os.Remove(cmd.Name())

	env, err := os.CreateTemp("", "env")
	if err != nil {
		return err
	}
	defer os.Remove(env.Name())

	// TODO for now we will ignore the cmd and env from the docker image
	for _, c := range cmds {
		fmt.Fprintf(cmd, "%s\n", c)
	}
	for k, v := range envs {
		fmt.Fprintf(env, "%s=%s\n", k, v)
	}

	customer, err := generateCustomer(image, cmd.Name(), env.Name())
	if err != nil {
		return err
	}
	defer os.Remove(customer.Name())

	bootstrapRamdisk := filepath.Join(artifactsDir, "bootstrap-initrd.img")
	customerRamdisk := filepath.Join(artifactsDir, "customer-initrd.img")

	command := execCommand(filepath.Join(blobsPath, "linuxkit"),
		"build",
		"-name",
		filepath.Join(artifactsDir, "bootstrap"),
		"-format",
		"kernel+initrd",
		bootstrap.Name(),
	)
	if err = command.Run(); err != nil {
		return err
	}

	command = execCommand(filepath.Join(blobsPath, "linuxkit"),
		"build",
		"-name",
		filepath.Join(artifactsDir, "customer"),
		"-format",
		"kernel+initrd",
		"-prefix",
		"rootfs/",
		customer.Name(),
	)
	if err = command.Run(); err != nil {
		return err
	}

	cmdline, err := ioutil.ReadFile(filepath.Join(blobsPath, "cmdline"))
	if err != nil {
		return err
	}
	command = execCommand("eif_build",
		"--kernel",
		filepath.Join(blobsPath, "bzImage"),
		"--kernel_config",
		filepath.Join(blobsPath, "bzImage.config"),
		"--cmdline",
		string(cmdline),
		"--ramdisk",
		bootstrapRamdisk,
		"--ramdisk",
		customerRamdisk,
		"--output",
		output,
	)
	if err = command.Run(); err != nil {
		return err
	}
	return nil
}

func execCommand(name string, arg ...string) *exec.Cmd {
	fmt.Println("Running:", name, arg)

	command := exec.Command(name, arg...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command
}

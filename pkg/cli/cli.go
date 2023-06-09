package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

type EnclaveConfig struct {
	EnclaveName string `json:"enclave_name,omitempty"`
	CPUCount    int    `json:"cpu_count,omitempty"`
	CPUIds      []int  `json:"cpu_ids,omitempty"`
	MemoryMib   int    `json:"memory_mib"`
	EifPath     string `json:"eif_path"`
	EnclaveCid  int    `json:"enclave_cid,omitempty"`
	DebugMode   bool   `json:"debug_mode,omitempty"`
}

type EnclaveInfo struct {
	EnclaveName  string `json:"EnclaveName"`
	EnclaveID    string `json:"EnclaveID"`
	ProcessID    int    `json:"ProcessID"`
	EnclaveCID   int    `json:"EnclaveCID"`
	NumberOfCPUs int    `json:"NumberOfCPUs"`
	CPUIDs       []int  `json:"CPUIDs"`
	MemoryMiB    int    `json:"MemoryMiB"`
	State        string `json:"State"`
	Flags        string `json:"Flags"`
}

type TerminationResponse struct {
	EnclaveID  string `json:"EnclaveID"`
	Terminated bool   `json:"Terminated"`
}

type EifInfo struct {
	EifVersion   int `json:"EifVersion"`
	Measurements struct {
		HashAlgorithm string `json:"HashAlgorithm"`
		Pcr0          string `json:"PCR0"`
		Pcr1          string `json:"PCR1"`
		Pcr2          string `json:"PCR2"`
	} `json:"Measurements"`
	IsSigned     bool   `json:"IsSigned"`
	CheckCRC     bool   `json:"CheckCRC"`
	ImageName    string `json:"ImageName"`
	ImageVersion string `json:"ImageVersion"`
	Metadata     struct {
		BuildTime        time.Time   `json:"BuildTime"`
		BuildTool        string      `json:"BuildTool"`
		BuildToolVersion string      `json:"BuildToolVersion"`
		OperatingSystem  string      `json:"OperatingSystem"`
		KernelVersion    string      `json:"KernelVersion"`
		DockerInfo       interface{} `json:"DockerInfo"`
	} `json:"Metadata"`
}

func RunEnclave(c *EnclaveConfig) (*EnclaveInfo, error) {
	file, err := os.CreateTemp("", "enclaveconfig")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	data, _ := json.MarshalIndent(c, "", " ")
	if _, err := file.Write(data); err != nil {
		return nil, err
	}

	info := new(EnclaveInfo)
	err = run(&info, '{', "nitro-cli", "run-enclave", "--config", file.Name())
	return info, err
}

func DescribeEnclaves() ([]EnclaveInfo, error) {
	info := new([]EnclaveInfo)
	err := run(&info, '[', "nitro-cli", "describe-enclaves")
	return *info, err
}

func TerminateEnclave(enclaveID string) (*TerminationResponse, error) {
	resp := new(TerminationResponse)
	err := run(&resp, '{', "nitro-cli", "terminate-enclave", enclaveID)
	return resp, err
}

func DescribeEif(eif string) (*EifInfo, error) {
	info := new(EifInfo)
	err := run(&info, '{', "nitro-cli", "describe-eif", "--eif-path", eif)
	return info, err
}

func run(v any, stop byte, name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	buf := new(bytes.Buffer)
	cmd.Stdout = buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	reader := bufio.NewReader(buf)
	if _, err := reader.ReadString(stop); err != nil {
		return err
	}
	if err := reader.UnreadByte(); err != nil {
		return err
	}

	buf = new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		return err
	}
	if err := json.Unmarshal(buf.Bytes(), v); err != nil {
		return err
	}
	return nil
}

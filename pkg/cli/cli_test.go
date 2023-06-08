package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunEnclave(t *testing.T) {
	info := new(EnclaveInfo)
	err := run(&info, '{', "/bin/echo", `Start allocating memory...
Started enclave with enclave-cid: 10, memory: 1600 MiB, cpu-ids: [1, 3]
{
    "EnclaveName": "my_enclave",
    "EnclaveID": "i-abc12345def67890a-enc9876abcd543210ef12",
    "ProcessID": 12345,
    "EnclaveCID": 10,
    "NumberOfCPUs": 2,
    "CPUIDs": [
        1,
        3
    ],
    "MemoryMiB": 1600
}`)
	assert.Nil(t, err)

	expected := EnclaveInfo{
		EnclaveName:  "my_enclave",
		EnclaveID:    "i-abc12345def67890a-enc9876abcd543210ef12",
		ProcessID:    12345,
		EnclaveCID:   10,
		NumberOfCPUs: 2,
		CPUIDs:       []int{1, 3},
		MemoryMiB:    1600,
	}
	assert.Equal(t, *info, expected, "they should be equal")
}

func TestDescribeEnclaves(t *testing.T) {
	info := new([]EnclaveInfo)
	err := run(&info, '[', "/bin/echo", `[
    {
        "EnclaveName": "my_enclave",
        "EnclaveID": "i-abc12345def67890a-enc9876abcd543210ef12",
        "ProcessID": 12345,
        "EnclaveCID": 10,
        "NumberOfCPUs": 2,
        "CPUIDs": [
            1,
            3
        ],
        "MemoryMiB": 1600,
        "State": "RUNNING",
        "Flags": "NONE"
    }
]`)
	assert.Nil(t, err)

	expected := []EnclaveInfo{EnclaveInfo{
		EnclaveName:  "my_enclave",
		EnclaveID:    "i-abc12345def67890a-enc9876abcd543210ef12",
		ProcessID:    12345,
		EnclaveCID:   10,
		NumberOfCPUs: 2,
		CPUIDs:       []int{1, 3},
		MemoryMiB:    1600,
		State:        "RUNNING",
		Flags:        "NONE",
	}}
	assert.Equal(t, *info, expected, "they should be equal")
}

func TestTerminateEnclave(t *testing.T) {
	resp := new(TerminationResponse)
	err := run(&resp, '{', "/bin/echo", `Successfully terminated enclave i-abc12345def67890a-enc9876abcd543210ef12.
{
  "EnclaveID": "i-abc12345def67890a-enc9876abcd543210ef12",
  "Terminated": true
}`)
	assert.Nil(t, err)

	expected := TerminationResponse{
		EnclaveID:  "i-abc12345def67890a-enc9876abcd543210ef12",
		Terminated: true,
	}
	assert.Equal(t, *resp, expected, "they should be equal")
}

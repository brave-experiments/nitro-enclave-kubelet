package node

import (
	"time"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/utils/smt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	MiB int64 = 1024 * 1024
	// Default container resource limits.
	containerDefaultCPULimit    int64 = 2
	containerDefaultMemoryLimit int64 = 512 // * MiB
)

var smtActive bool

func init() {
	smtActive, _ = smt.Active()
}

// Container is the representation of a Kubernetes container in an enclave.
type container struct {
	definition containerDefinition

	startTime  time.Time
	finishTime time.Time
}

// containerDefinition
type containerDefinition struct {
	Name        string
	Image       string
	EntryPoint  []string
	Command     []string
	Environment map[string]string
	// CPUs in integer cores
	Cpu int64
	// Memory in MiB
	Memory int64
}

// NewContainer creates a new container from a Kubernetes container spec.
func newContainer(spec *corev1.Container) (*container, error) {
	var cntr container

	// Translate the Kubernetes container spec to a container definition.
	cntr.definition = containerDefinition{
		Name:       spec.Name,
		Image:      spec.Image,
		EntryPoint: spec.Command,
		Command:    spec.Args,
	}

	// Add environment variables.
	if spec.Env != nil {
		for _, env := range spec.Env {
			cntr.definition.Environment[env.Name] = env.Value
		}
	}

	// Translate the Kubernetes container resource requirements to enclave units.
	cntr.setResourceRequirements(&spec.Resources)

	return &cntr, nil
}

// SetResourceRequirements translates Kubernetes container resource requirements to enclave units.
func (cntr *container) setResourceRequirements(reqs *corev1.ResourceRequirements) {
	//
	// Kubernetes container resource requirements consist of "limits" and "requests" for each
	// resource type. Limits are the maximum amount of resources allowed. Requests are the minimum
	// amount of resources reserved for the container. Both are optional. If requests are omitted,
	// they default to limits. If limits are also omitted, they both default to an
	// implementation-defined value.
	//

	// Use the defaults if the container does not have any resource requirements.
	cpu := containerDefaultCPULimit
	memory := containerDefaultMemoryLimit

	// Compute CPU requirements.
	if reqs != nil {
		var quantity resource.Quantity
		var ok bool

		// Enclaves do not share resources with other tasks. Therefore the task and each
		// container in it must be allocated their resource limits. Hence limits are preferred
		// over requests.
		if reqs.Limits != nil {
			quantity, ok = reqs.Limits[corev1.ResourceCPU]
		}
		if !ok && reqs.Requests != nil {
			quantity, ok = reqs.Requests[corev1.ResourceCPU]
		}
		if ok {
			cpu = quantity.ScaledValue(resource.Milli) / 1000
			// If SMT is active we must specify CPUs in pairs
			if smtActive {
				cpu = cpu * 2
			}
		}
	}

	// Compute memory requirements.
	if reqs != nil {
		var reqQuantity resource.Quantity
		var limQuantity resource.Quantity
		var reqOk bool
		var limOk bool

		// Find the memory request and limit, if available.
		if reqs.Requests != nil {
			reqQuantity, reqOk = reqs.Requests[corev1.ResourceMemory]
		}
		if reqs.Limits != nil {
			limQuantity, limOk = reqs.Limits[corev1.ResourceMemory]
		}

		// If one is omitted, use the other one's value.
		if !limOk && reqOk {
			limQuantity = reqQuantity
		} else if !reqOk && limOk {
			reqQuantity = limQuantity
		}

		// If at least one is specified...
		if reqOk || limOk {
			// Convert memory unit from bytes to MiBs, rounding up to the next MiB.
			// This is necessary because enclave memory is specified in MiBs.
			memory = (limQuantity.Value() + MiB - 1) / MiB
		}
	}

	// Set final values.
	cntr.definition.Cpu = cpu
	cntr.definition.Memory = memory
}

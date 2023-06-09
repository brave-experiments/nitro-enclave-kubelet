package node

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/build"
	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/cli"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

const (
	// Prefixes for objects created in Fargate.
	enclaveNamePrefix = "vk-podspec"

	// Enclave state strings.
	enclaveStateTerminating = "TERMINATING"
	enclaveStateRunning     = "RUNNING"
)

// Pod is the representation of a Kubernetes pod as a Nitro Enclave.
type Pod struct {
	// Kubernetes pod properties.
	namespace string
	name      string
	uid       k8sTypes.UID

	// Enclave properties.
	info       cli.EnclaveInfo
	config     cli.EnclaveConfig
	image      string
	node       *Node
	containers map[string]*container
}

// NewPod creates a new Kubernetes pod as a Nitro Enclave.
func NewPod(ctx context.Context, node *Node, pod *corev1.Pod) (*Pod, error) {
	// Initialize the pod.
	nitroPod := &Pod{
		namespace:  pod.Namespace,
		name:       pod.Name,
		uid:        pod.UID,
		node:       node,
		containers: make(map[string]*container),
	}

	tag := nitroPod.buildEnclaveNameTag()
	nitroPod.config.EnclaveName = tag

	if len(pod.Spec.Containers) > 1 {
		return nil, fmt.Errorf("launching more than 1 container is unsupported")
	}

	// For each container in the pod...
	for _, containerSpec := range pod.Spec.Containers {
		// Create a container definition.
		cntr, err := newContainer(&containerSpec)
		if err != nil {
			return nil, err
		}

		// Add the container's resource requirements to its pod's total resource requirements.
		nitroPod.config.CPUCount += cntr.definition.Cpu
		nitroPod.config.MemoryMib += cntr.definition.Memory

		// Insert the container to its pod.
		nitroPod.containers[containerSpec.Name] = cntr
	}

	// Register the task definition with Fargate.
	log.G(ctx).Infof("produced EnclaveInfo %+v", nitroPod.config)

	if node != nil {
		node.InsertPod(nitroPod, tag)
	}

	return nitroPod, nil
}

// NewPodFromTag creates a new pod identified by a tag.
func NewPodFromTag(node *Node, tag string) (*Pod, error) {
	data := strings.Split(tag, "_")

	if len(data) < 3 ||
		data[0] != enclaveNamePrefix {
		return nil, fmt.Errorf("invalid tag")
	}

	pod := &Pod{
		namespace:  data[1],
		name:       data[2],
		node:       node,
		containers: make(map[string]*container),
	}

	return pod, nil
}

// Start deploys and runs a Kubernetes pod in an enclave.
func (pod *Pod) Start(ctx context.Context) error {
	// Build the enclave image
	var d containerDefinition
	for _, v := range pod.containers {
		d = v.definition
	}

	eif, err := os.CreateTemp("", pod.config.EnclaveName)
	if err != nil {
		return err
	}
	defer os.Remove(eif.Name())

	err = build.BuildEif("/usr/share/nitro_enclaves/blobs/", d.Image, append(d.EntryPoint, d.Command...), d.Environment, eif.Name())
	if err != nil {
		err = fmt.Errorf("failed to build enclave image: %v", err)
		return err
	}

	pod.config.EifPath = eif.Name()
	// FIXME always debug for now
	pod.config.DebugMode = true
	log.G(ctx).Infof("built eif image %+v", pod.config)

	// Start the enclave.
	info, err := cli.RunEnclave(&pod.config)
	if err != nil {
		err = fmt.Errorf("failed to run enclave: %v", err)
		return err
	}

	log.G(ctx).Infof("launched enclave %+v", info)
	// Save the enclave info
	pod.info = *info

	return nil
}

// Stop stops a running Kubernetes pod running as an enclave.
func (pod *Pod) Stop() error {
	_, err := cli.TerminateEnclave(pod.info.EnclaveID)
	if err != nil {
		err = fmt.Errorf("failed to stop enclave: %v", err)
		return err
	}

	// Remove the pod from its node.
	if pod.node != nil {
		pod.node.RemovePod(pod.buildEnclaveNameTag())
	}

	return nil
}

// GetSpec returns the specification of a Kubernetes pod on Fargate.
func (pod *Pod) GetSpec() (*corev1.Pod, error) {
	containers := make([]corev1.Container, 0, len(pod.containers))

	for _, c := range pod.containers {
		cntr := corev1.Container{
			Name:    c.definition.Name,
			Image:   c.definition.Image,
			Command: c.definition.EntryPoint,
			Args:    c.definition.Command,
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", c.definition.Cpu)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", c.definition.Memory)),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", c.definition.Cpu)),
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", c.definition.Memory)),
				},
			},
			//Ports: make([]corev1.ContainerPort, 0, len(cntrDef.PortMappings)),
			Env: make([]corev1.EnvVar, 0, len(c.definition.Environment)),
		}

		//for _, mapping := range cntrDef.PortMappings {
		//cntr.Ports = append(cntr.Ports, corev1.ContainerPort{
		//ContainerPort: int32(*mapping.ContainerPort),
		//HostPort:      int32(*mapping.HostPort),
		//Protocol:      corev1.ProtocolTCP,
		//})
		//}

		for k, v := range c.definition.Environment {
			cntr.Env = append(cntr.Env, corev1.EnvVar{
				Name:  k,
				Value: v,
			})
		}

		containers = append(containers, cntr)
	}

	annotations := make(map[string]string)

	//if pod.taskRoleArn != "" {
	//annotations[taskRoleAnnotation] = pod.taskRoleArn
	//}

	podSpec := corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   pod.namespace,
			Name:        pod.name,
			UID:         pod.uid,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			NodeName:   pod.node.name,
			Volumes:    []corev1.Volume{},
			Containers: containers,
		},
		Status: pod.GetStatus(),
	}

	return &podSpec, nil
}

// GetStatus returns the status of a Kubernetes pod running as an enclave.
func (pod *Pod) GetStatus() corev1.PodStatus {
	enclaves, err := cli.DescribeEnclaves()
	if err != nil {
		return corev1.PodStatus{Phase: corev1.PodUnknown}
	}

	for _, info := range enclaves {
		if info.EnclaveName == pod.buildEnclaveNameTag() {
			if info.State == enclaveStateRunning || info.State == enclaveStateTerminating {
				return corev1.PodStatus{Phase: corev1.PodRunning}
			}
		}
	}

	return corev1.PodStatus{Phase: corev1.PodFailed}
}

// buildEnclaveNameTag returns the enclave name tag for this pod.
func (pod *Pod) buildEnclaveNameTag() string {
	return buildEnclaveNameTag(pod.namespace, pod.name)
}

// buildEnclaveNameTag builds an enclave name tag from its components.
func buildEnclaveNameTag(namespace string, name string) string {
	// namespace_podname
	return fmt.Sprintf("%s_%s_%s", enclaveNamePrefix, namespace, name)
}
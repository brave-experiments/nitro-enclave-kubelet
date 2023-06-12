package node

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/build"
	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/cli"
	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/utils/nitro"
	"github.com/mdlayher/vsock"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sTypes "k8s.io/apimachinery/pkg/types"
)

const (
	// Prefixes for objects created in Fargate.
	enclaveNamePrefix = "vk-podspec"

	// Enclave state strings.
	enclaveStateTerminating = "TERMINATING"
	enclaveStateRunning     = "RUNNING"
)

type portMapping struct {
	containerPort int32
	hostPort      int32
}

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
	ports      []portMapping
	containers map[string]*container

	// Utilities
	listeners []net.Listener
}

func IsOwnedBy(pod *corev1.Pod, gvks []schema.GroupVersionKind) bool {
	for _, ignoredOwner := range gvks {
		for _, owner := range pod.ObjectMeta.OwnerReferences {
			if owner.APIVersion == ignoredOwner.GroupVersion().String() && owner.Kind == ignoredOwner.Kind {
				return true
			}
		}
	}
	return false
}

func IsOwnedByDaemonSet(pod *corev1.Pod) bool {
	return IsOwnedBy(pod, []schema.GroupVersionKind{
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
	})
}

// NewPod creates a new Kubernetes pod as a Nitro Enclave.
func NewPod(ctx context.Context, node *Node, pod *corev1.Pod) (*Pod, error) {
	if IsOwnedByDaemonSet(pod) {
		return nil, fmt.Errorf("daemonsets are not supported")
	}

	// Initialize the pod.
	nitroPod := &Pod{
		namespace:  pod.Namespace,
		name:       pod.Name,
		uid:        pod.UID,
		node:       node,
		ports:      make([]portMapping, 0),
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

		for _, port := range containerSpec.Ports {
			nitroPod.ports = append(nitroPod.ports, portMapping{
				containerPort: port.ContainerPort,
				hostPort:      port.HostPort,
			})
		}

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
	log.G(ctx).Infof("built eif %s %+v %+v %s", d.Image, append(d.EntryPoint, d.Command...), d.Environment, eif.Name())

	pod.config.EifPath = eif.Name()
	// FIXME always debug for now
	pod.config.DebugMode = true

	// Start the enclave.
	info, err := cli.RunEnclave(&pod.config)
	if err != nil {
		err = fmt.Errorf("failed to run enclave: %v", err)
		return err
	}
	log.G(ctx).Infof("launched enclave %+v", info)

	// Start the TCP proxies
	for _, mapping := range pod.ports {
		proxy := nitro.TCPProxy(uint32(info.EnclaveCID), uint32(mapping.containerPort))
		listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", mapping.hostPort))
		if err != nil {
			log.G(ctx).Errorf("failed to start proxy listener")
			continue
		}
		pod.listeners = append(pod.listeners, listener)
		proxy.Serve(listener)
	}

	// Start the log server
	// FIXME don't just write logs to stdout
	logPort := uint32(info.EnclaveCID + 10000)
	listener, err := vsock.Listen(logPort, &vsock.Config{})
	if err != nil {
		log.G(ctx).Errorf("failed to start log server listener")
	} else {
		pod.listeners = append(pod.listeners, listener)
		logserve := nitro.NewVsockLogServer(ctx, os.Stdout, logPort)
		go func() {
			if err := logserve.Serve(listener); err != nil {
				log.G(ctx).Errorf("failed to start log server")
			}
		}()
	}

	// Save the enclave info
	pod.info = *info

	return nil
}

// Stop stops a running Kubernetes pod running as an enclave.
func (pod *Pod) Stop() error {
	if pod.GetStatus().Phase == corev1.PodRunning {
		_, err := cli.TerminateEnclave(pod.info.EnclaveID)
		if err != nil {
			err = fmt.Errorf("failed to stop enclave: %v", err)
			return err
		}
	}

	if len(pod.listeners) > 0 {
		for _, listener := range pod.listeners {
			listener.Close()
		}
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
	status := corev1.PodStatus{Phase: corev1.PodUnknown}
	enclaves, err := cli.DescribeEnclaves()
	if err != nil {
		return status
	}

	for _, info := range enclaves {
		if info.EnclaveName == pod.buildEnclaveNameTag() {
			if info.State == enclaveStateRunning || info.State == enclaveStateTerminating {
				status.Phase = corev1.PodRunning
				status.HostIP = pod.node.ip
				status.PodIP = pod.node.ip
				status.Conditions = []corev1.PodCondition{
					corev1.PodCondition{Type: corev1.PodInitialized, Status: "True"},
					corev1.PodCondition{Type: corev1.PodReady, Status: "True"},
					corev1.PodCondition{Type: corev1.ContainersReady, Status: "True"},
					corev1.PodCondition{Type: corev1.PodScheduled, Status: "True"},
				}
				return status
			}
		}
	}

	status.Phase = corev1.PodFailed
	return status
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

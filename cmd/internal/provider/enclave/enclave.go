package enclave

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	enclavenode "github.com/brave-experiments/nitro-enclave-kubelet/pkg/node"
	dto "github.com/prometheus/client_model/go"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	stats "github.com/virtual-kubelet/virtual-kubelet/node/api/statsv1alpha1"
	"github.com/virtual-kubelet/virtual-kubelet/trace"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Provider configuration defaults.
	defaultCPUCapacity            = "4"
	defaultMemoryCapacity         = "1024Mi"
	defaultReservedCPUCapacity    = "2"
	defaultReservedMemoryCapacity = "512Mi"
	defaultPodCapacity            = "10"
	defaultNitroEnclaveCapacity   = "1"

	// Values used in tracing as attribute keys.
	namespaceKey     = "namespace"
	nameKey          = "name"
	containerNameKey = "containerName"
)

var (
	errNotImplemented = fmt.Errorf("not implemented by Nitro Enclave provider")
)

// EnclaveProvider implements the virtual-kubelet provider interface and stores pods in memory.
type EnclaveProvider struct { //nolint:golint
	nodeName           string
	operatingSystem    string
	internalIP         string
	daemonEndpointPort int32

	node      *enclavenode.Node
	config    EnclaveConfig
	startTime time.Time
	//notifier  func(*v1.Pod)
}

// EnclaveConfig contains a enclave virtual-kubelet's configurable parameters.
type EnclaveConfig struct { //nolint:golint
	CPU            string            `json:"cpu,omitempty"`
	Memory         string            `json:"memory,omitempty"`
	ReservedCPU    string            `json:"reservedCpu,omitempty"`
	ReservedMemory string            `json:"reservedMemory,omitempty"`
	Pods           string            `json:"pods,omitempty"`
	Others         map[string]string `json:"others,omitempty"`
	ProviderID     string            `json:"providerID,omitempty"`
}

// NewEnclaveProviderEnclaveConfig creates a new EnclaveV0Provider. Enclave legacy provider does not implement the new asynchronous podnotifier interface
func NewEnclaveProviderEnclaveConfig(ctx context.Context, config EnclaveConfig, nodeName, operatingSystem string, internalIP string, daemonEndpointPort int32) (*EnclaveProvider, error) {
	// set defaults
	if config.CPU == "" {
		config.CPU = defaultCPUCapacity
	}
	if config.ReservedCPU == "" {
		config.ReservedCPU = defaultReservedCPUCapacity
	}
	if config.Memory == "" {
		config.Memory = defaultMemoryCapacity
	}
	if config.ReservedMemory == "" {
		config.ReservedMemory = defaultReservedMemoryCapacity
	}
	if config.Pods == "" {
		config.Pods = defaultPodCapacity
	}

	en, err := enclavenode.NewNode(ctx, &enclavenode.NodeConfig{Name: nodeName}, internalIP)
	if err != nil {
		return nil, err
	}

	provider := EnclaveProvider{
		nodeName:           nodeName,
		operatingSystem:    operatingSystem,
		internalIP:         internalIP,
		daemonEndpointPort: daemonEndpointPort,
		node:               en,
		config:             config,
		startTime:          time.Now(),
	}

	return &provider, nil
}

// NewEnclaveProvider creates a new EnclaveProvider, which implements the PodNotifier interface
func NewEnclaveProvider(ctx context.Context, providerConfig, nodeName, operatingSystem string, internalIP string, daemonEndpointPort int32) (*EnclaveProvider, error) {
	config, err := loadConfig(providerConfig, nodeName)
	if err != nil {
		return nil, err
	}

	return NewEnclaveProviderEnclaveConfig(ctx, config, nodeName, operatingSystem, internalIP, daemonEndpointPort)
}

// loadConfig loads the given json configuration files.
func loadConfig(providerConfig, nodeName string) (config EnclaveConfig, err error) {
	data, err := os.ReadFile(providerConfig)
	if err != nil {
		return config, err
	}
	configMap := map[string]EnclaveConfig{}
	err = json.Unmarshal(data, &configMap)
	if err != nil {
		return config, err
	}
	if _, exist := configMap[nodeName]; exist {
		config = configMap[nodeName]
		if config.CPU == "" {
			config.CPU = defaultCPUCapacity
		}
		if config.ReservedCPU == "" {
			config.ReservedCPU = defaultReservedCPUCapacity
		}
		if config.Memory == "" {
			config.Memory = defaultMemoryCapacity
		}
		if config.ReservedMemory == "" {
			config.ReservedMemory = defaultReservedMemoryCapacity
		}
		if config.Pods == "" {
			config.Pods = defaultPodCapacity
		}
	}

	if _, err = resource.ParseQuantity(config.CPU); err != nil {
		return config, fmt.Errorf("Invalid CPU value %v", config.CPU)
	}
	if _, err = resource.ParseQuantity(config.Memory); err != nil {
		return config, fmt.Errorf("Invalid memory value %v", config.Memory)
	}
	if _, err = resource.ParseQuantity(config.Pods); err != nil {
		return config, fmt.Errorf("Invalid pods value %v", config.Pods)
	}
	for _, v := range config.Others {
		if _, err = resource.ParseQuantity(v); err != nil {
			return config, fmt.Errorf("Invalid other value %v", v)
		}
	}
	return config, nil
}

// CreatePod accepts a Pod definition and launches it as an enclave
func (p *EnclaveProvider) CreatePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "CreatePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive CreatePod %q", pod.Name)

	enclavePod, err := enclavenode.NewPod(ctx, p.node, pod)
	if err != nil {
		log.G(ctx).Errorf("Failed to create pod: %v.\n", err)
		return err
	}

	err = enclavePod.Start(ctx)
	if err != nil {
		log.G(ctx).Errorf("Failed to start pod: %v.\n", err)
		return err
	}

	pod.Status = enclavePod.GetStatus()
	//p.notifier(pod)

	return nil
}

// UpdatePod accepts a Pod definition and updates its reference.
func (p *EnclaveProvider) UpdatePod(ctx context.Context, pod *v1.Pod) error {
	ctx, span := trace.StartSpan(ctx, "UpdatePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive UpdatePod %q", pod.Name)

	// TODO add limited support to allow recovering from kubelet restart?
	return errNotImplemented
}

// DeletePod deletes the pod, terminating the running enclave.
func (p *EnclaveProvider) DeletePod(ctx context.Context, pod *v1.Pod) (err error) {
	ctx, span := trace.StartSpan(ctx, "DeletePod")
	defer span.End()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, pod.Namespace, nameKey, pod.Name)

	log.G(ctx).Infof("receive DeletePod %q", pod.Name)

	enclavePod, err := p.node.GetPod(pod.Namespace, pod.Name)
	if err != nil {
		log.G(ctx).Errorf("Failed to get pod: %v.\n", err)
		return err
	}

	err = enclavePod.Stop()
	if err != nil {
		log.G(ctx).Errorf("Failed to stop pod: %v.\n", err)
		return err
	}

	//p.notifier(pod)

	return nil
}

// GetPod returns a pod by name that is running as an enclave
func (p *EnclaveProvider) GetPod(ctx context.Context, namespace, name string) (pod *v1.Pod, err error) {
	ctx, span := trace.StartSpan(ctx, "GetPod")
	defer func() {
		span.SetStatus(err)
		span.End()
	}()

	// Add the pod's coordinates to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, name)

	log.G(ctx).Infof("receive GetPod %q", name)

	enclavePod, err := p.node.GetPod(namespace, name)
	if err != nil {
		log.G(ctx).Errorf("Failed to get pod: %v.\n", err)
		return nil, err
	}

	spec, err := enclavePod.GetSpec()
	if err != nil {
		log.G(ctx).Errorf("Failed to get pod spec: %v.\n", err)
		return nil, err
	}

	return spec, nil
}

// GetContainerLogs retrieves the logs of a container by name from the provider.
func (p *EnclaveProvider) GetContainerLogs(ctx context.Context, namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	ctx, span := trace.StartSpan(ctx, "GetContainerLogs")
	defer span.End()

	// Add pod and container attributes to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, podName, containerNameKey, containerName)

	log.G(ctx).Infof("receive GetContainerLogs %q", podName)

	return p.node.GetContainerLogs(namespace, podName, containerName, opts)
}

// RunInContainer executes a command in a container in the pod, copying data
// between in/out/err and the container's stdin/stdout/stderr.
func (p *EnclaveProvider) RunInContainer(ctx context.Context, namespace, name, container string, cmd []string, attach api.AttachIO) error {
	log.G(context.TODO()).Infof("receive ExecInContainer %q", container)
	return nil
}

// AttachToContainer attaches to the executing process of a container in the pod, copying data
// between in/out/err and the container's stdin/stdout/stderr.
func (p *EnclaveProvider) AttachToContainer(ctx context.Context, namespace, name, container string, attach api.AttachIO) error {
	log.G(ctx).Infof("receive AttachToContainer %q", container)
	return nil
}

// GetPodStatus returns the status of a pod by name that is "running".
// returns nil if a pod by that name is not found.
func (p *EnclaveProvider) GetPodStatus(ctx context.Context, namespace, name string) (*v1.PodStatus, error) {
	ctx, span := trace.StartSpan(ctx, "GetPodStatus")
	defer span.End()

	// Add namespace and name as attributes to the current span.
	ctx = addAttributes(ctx, span, namespaceKey, namespace, nameKey, name)

	log.G(ctx).Infof("receive GetPodStatus %q", name)

	pod, err := p.GetPod(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return &pod.Status, nil
}

// GetPods returns a list of all pods known to be "running".
func (p *EnclaveProvider) GetPods(ctx context.Context) ([]*v1.Pod, error) {
	ctx, span := trace.StartSpan(ctx, "GetPods")
	defer span.End()

	log.G(ctx).Info("receive GetPods")

	pods, err := p.node.GetPods()
	if err != nil {
		log.G(ctx).Errorf("Failed to get pods: %v.\n", err)
		return nil, err
	}

	var result []*v1.Pod
	var podNames []string

	for _, pod := range pods {
		spec, err := pod.GetSpec()
		if err != nil {
			log.G(ctx).Errorf("Failed to get pod spec: %v.\n", err)
			continue
		}

		result = append(result, spec)
		podNames = append(podNames, fmt.Sprintf("%s/%s", spec.Namespace, spec.Name))
	}

	log.G(ctx).Info("Responding to GetPods: %+v.\n", podNames)

	return result, nil
}

func (p *EnclaveProvider) ConfigureNode(ctx context.Context, n *v1.Node) { //nolint:golint
	ctx, span := trace.StartSpan(ctx, "enclave.ConfigureNode") //nolint:staticcheck,ineffassign
	defer span.End()

	if p.config.ProviderID != "" {
		n.Spec.ProviderID = p.config.ProviderID
	}
	n.Status.Capacity = p.capacity()
	n.Status.Allocatable = p.allocatable()
	n.Status.Conditions = p.nodeConditions()
	n.Status.Addresses = p.nodeAddresses()
	n.Status.DaemonEndpoints = p.nodeDaemonEndpoints()
	os := p.operatingSystem
	if os == "" {
		os = "linux"
	}
	n.Status.NodeInfo.OperatingSystem = os
	n.Status.NodeInfo.Architecture = "amd64"
	delete(n.ObjectMeta.Labels, "kubernetes.io/role")

	// FIXME
	n.ObjectMeta.Labels["eks.amazonaws.com/compute-type"] = "fargate"
	//n.ObjectMeta.Labels["alpha.service-controller.kubernetes.io/exclude-balancer"] = "true"
	//n.ObjectMeta.Labels["node.kubernetes.io/exclude-from-external-load-balancers"] = "true"
}

// Capacity returns a resource list containing the capacity limits.
func (p *EnclaveProvider) capacity() v1.ResourceList {
	rl := v1.ResourceList{
		"cpu":    resource.MustParse(p.config.CPU),
		"memory": resource.MustParse(p.config.Memory),
		"pods":   resource.MustParse(p.config.Pods),
		"aws.ec2.nitro/nitro_enclaves": resource.MustParse(defaultNitroEnclaveCapacity),
	}
	for k, v := range p.config.Others {
		rl[v1.ResourceName(k)] = resource.MustParse(v)
	}
	return rl
}

// Allocatable returns a resource list containing the allocatable limits.
func (p *EnclaveProvider) allocatable() v1.ResourceList {
	rl := p.capacity()
	// Reserve cpu and memory for non-enclave processes
	rl.Cpu().Sub(resource.MustParse(p.config.ReservedCPU))
	rl.Memory().Sub(resource.MustParse(p.config.ReservedMemory))
	return rl
}

// NodeConditions returns a list of conditions (Ready, OutOfDisk, etc), for updates to the node status
// within Kubernetes.
func (p *EnclaveProvider) nodeConditions() []v1.NodeCondition {
	// TODO: Make this configurable
	return []v1.NodeCondition{
		{
			Type:               "Ready",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletPending",
			Message:            "kubelet is pending.",
		},
		{
			Type:               "OutOfDisk",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientDisk",
			Message:            "kubelet has sufficient disk space available",
		},
		{
			Type:               "MemoryPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasSufficientMemory",
			Message:            "kubelet has sufficient memory available",
		},
		{
			Type:               "DiskPressure",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "KubeletHasNoDiskPressure",
			Message:            "kubelet has no disk pressure",
		},
		{
			Type:               "NetworkUnavailable",
			Status:             v1.ConditionFalse,
			LastHeartbeatTime:  metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "RouteCreated",
			Message:            "RouteController created a route",
		},
	}

}

// NodeAddresses returns a list of addresses for the node status
// within Kubernetes.
func (p *EnclaveProvider) nodeAddresses() []v1.NodeAddress {
	return []v1.NodeAddress{
		{
			Type:    "InternalIP",
			Address: p.internalIP,
		},
	}
}

// NodeDaemonEndpoints returns NodeDaemonEndpoints for the node status
// within Kubernetes.
func (p *EnclaveProvider) nodeDaemonEndpoints() v1.NodeDaemonEndpoints {
	return v1.NodeDaemonEndpoints{
		KubeletEndpoint: v1.DaemonEndpoint{
			Port: p.daemonEndpointPort,
		},
	}
}

// NotifyPods is called to set a pod notifier callback function. This should be called before any operations are done
// within the provider.
//func (p *EnclaveProvider) NotifyPods(ctx context.Context, notifier func(*v1.Pod)) {
//p.notifier = notifier
//}

func (p *EnclaveProvider) GetMetricsResource(ctx context.Context) ([]*dto.MetricFamily, error) {
	return nil, errNotImplemented
}
func (p *EnclaveProvider) GetStatsSummary(ctx context.Context) (*stats.Summary, error) {
	return nil, errNotImplemented
}

// addAttributes adds the specified attributes to the provided span.
// attrs must be an even-sized list of string arguments.
// Otherwise, the span won't be modified.
// TODO: Refactor and move to a "tracing utilities" package.
func addAttributes(ctx context.Context, span trace.Span, attrs ...string) context.Context {
	if len(attrs)%2 == 1 {
		return ctx
	}
	for i := 0; i < len(attrs); i += 2 {
		ctx = span.WithField(ctx, attrs[i], attrs[i+1])
	}
	return ctx
}

package node

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/brave-experiments/nitro-enclave-kubelet/pkg/cli"
	"github.com/virtual-kubelet/virtual-kubelet/errdefs"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
)

// NodeConfig contains a node's configurable parameters
type NodeConfig struct {
	Name string
}

// Node represents an enclave enabled node.
type Node struct {
	name string
	ip   string
	pods map[string]*Pod
	sync.RWMutex
}

// NewNode creates a new Node object.
func NewNode(ctx context.Context, config *NodeConfig, internalIP string) (*Node, error) {
	// Initialize the node.
	node := &Node{
		name: config.Name,
		pods: make(map[string]*Pod),
		ip:   internalIP,
	}

	// Load existing pod state from enclaves to the local cache.
	err := node.loadPodState(ctx)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// LoadPodState rebuilds pod and container objects in this node by loading existing enclaves
func (n *Node) loadPodState(ctx context.Context) error {
	log.G(ctx).Infof("Loading pod state for node %s", n.name)

	enclaves, err := cli.DescribeEnclaves()
	if err != nil {
		err := fmt.Errorf("failed to load pod state: %v", err)
		return err
	}

	log.G(ctx).Infof("Found %d enclaves on node %s.", len(enclaves), n.name)

	pods := make(map[string]*Pod)

	// For each enclave running on this node...
	for _, info := range enclaves {
		// A pod's tag is stored in the enclave name
		tag := info.EnclaveName

		// Rebuild the pod object.
		// Not all enclaves are necessarily pods. Skip enclaves that do not have a valid tag.
		pod, err := NewPodFromTag(n, tag)
		if err != nil {
			log.G(ctx).Infof("Skipping unknown enclave %s (%s): %v", tag, info.EnclaveID, err)
			continue
		}

		pod.info = info

		log.G(ctx).Infof("Found pod %s/%s on node %s.", pod.namespace, pod.name, n.name)

		pods[tag] = pod
	}

	// Update local state.
	n.Lock()
	n.pods = pods
	n.Unlock()

	return nil
}

// GetPod returns a Kubernetes pod deployed on this node.
func (n *Node) GetPod(namespace string, name string) (*Pod, error) {
	n.RLock()
	defer n.RUnlock()

	tag := buildEnclaveNameTag(namespace, name)
	pod, ok := n.pods[tag]
	if !ok {
		return nil, errdefs.NotFoundf("pod %s/%s is not found", namespace, name)
	}

	return pod, nil
}

// GetPods returns all Kubernetes pods deployed on this node.
func (n *Node) GetPods() ([]*Pod, error) {
	n.RLock()
	defer n.RUnlock()

	pods := make([]*Pod, 0, len(n.pods))

	for _, pod := range n.pods {
		pods = append(pods, pod)
	}

	return pods, nil
}

// InsertPod inserts a Kubernetes pod to this node.
func (n *Node) InsertPod(pod *Pod, tag string) {
	n.Lock()
	defer n.Unlock()

	n.pods[tag] = pod
}

// RemovePod removes a Kubernetes pod from this node.
func (n *Node) RemovePod(tag string) {
	n.Lock()
	defer n.Unlock()

	delete(n.pods, tag)
}

type truncatedReader struct {
	r io.ReadCloser
}

func (tr truncatedReader) Read(p []byte) (n int, err error) {
	n, err = tr.r.Read(p)
	if err == io.EOF {
		err := tr.r.Close()
		if err != nil {
			return n, err
		}
	}
	return n, err
}

func (tr truncatedReader) Close() error {
	return tr.r.Close()
}

// GetContainerLogs returns the logs of a container from this node.
func (n *Node) GetContainerLogs(namespace, podName, containerName string, opts api.ContainerLogOpts) (io.ReadCloser, error) {
	tag := buildEnclaveNameTag(namespace, podName)
	pod, ok := n.pods[tag]
	if !ok {
		return nil, errdefs.NotFoundf("pod %s/%s is not found", namespace, podName)
	}

	// TODO add support for logging server, merge with console when available
	// FIXME bunch of weird bugs atm, switch to writing to a file in the background
	// FIXME only use console when enclave is running in debug mode
	r, err := cli.Console(pod.info.EnclaveID)
	if err != nil {
		return nil, err
	}
	if !opts.Follow {
		return truncatedReader{r}, nil
	}
	return r, nil
}

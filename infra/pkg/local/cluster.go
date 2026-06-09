// Package local provides helpers for managing local KIND (Kubernetes IN Docker)
// clusters used in development and CI environments. It is responsible for
// building KIND node configurations and resolving Kubernetes node image tags.
// It is NOT responsible for Pulumi resource lifecycle, remote cluster
// provisioning, or any persistent state. Key entry points: [buildNodes],
// [k8sNodeImage], [createKindCluster].
package local

import (
	"fmt"
	"strings"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// buildNodes returns a KIND node list consisting of one control-plane node
// followed by nodeCount worker nodes. Callers pass 0 to get a single-node
// cluster (control-plane only). The returned slice is always non-nil and has
// length 1+nodeCount.
func buildNodes(nodeCount int) []kindv1a4.Node {
	nodes := make([]kindv1a4.Node, 0, 1+nodeCount)
	nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.ControlPlaneRole})
	for i := 0; i < nodeCount; i++ {
		nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.WorkerRole})
	}
	return nodes
}

// k8sNodeImage converts a Kubernetes version string into a fully-qualified
// KIND node image reference of the form "kindest/node:vX.Y.Z". Accepted
// input formats: "1.29" (bare minor), "1.29.2" (with patch), "v1.29.0"
// (v-prefixed). A bare minor version (no patch component) is expanded to
// patch ".0".
//
// why: KIND requires an exact image tag including the patch component; callers
// that only know the minor version should not have to handle the ".0" expansion
// themselves.
func k8sNodeImage(version string) string {
	v := strings.TrimPrefix(version, "v")
	// why: bare minor versions like "1.29" have no patch segment; expand to
	// "1.29.0" so the resulting image tag is always in vX.Y.Z form.
	if strings.Count(v, ".") == 1 {
		v = v + ".0"
	}
	return "kindest/node:v" + v
}

// createKindCluster provisions a KIND cluster with the given name and topology,
// writes its kubeconfig, and returns the path to that kubeconfig file.
// It is idempotent: if a cluster with the given name already exists it returns
// the existing kubeconfig path without error.
//
// Implemented in Task 4; returns [ErrNotImplemented] until then.
func createKindCluster(name string, nodeCount int, k8sVersion string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

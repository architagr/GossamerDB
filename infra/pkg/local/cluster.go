// Package local provides helpers for managing local KIND (Kubernetes IN Docker)
// clusters used in development and CI environments. It is responsible for
// building KIND node configurations, resolving Kubernetes node image tags, and
// provisioning clusters with idempotent kubeconfig export. It is NOT
// responsible for Pulumi resource lifecycle, remote cluster provisioning, or
// any persistent state. Key entry points: [buildNodes], [k8sNodeImage],
// [createKindCluster].
package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
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

// alreadyExists reports whether a KIND cluster with the given name is present
// in the list returned by provider.List(). Returns (false, err) if listing
// fails; callers should treat any error as a hard failure, not as "does not
// exist".
func alreadyExists(provider *kindcluster.Provider, name string) (bool, error) {
	clusters, err := provider.List()
	if err != nil {
		return false, fmt.Errorf("list clusters: %w", err)
	}
	for _, c := range clusters {
		if c == name {
			return true, nil
		}
	}
	return false, nil
}

// createKindCluster provisions a KIND cluster with the given name and topology,
// writes its kubeconfig to ~/.kube/gossamerdb-<name>.yaml, and returns that
// path. It is idempotent: if a cluster with the given name already exists it
// skips creation, retrieves the existing kubeconfig, and returns the path
// without error.
//
// nodeCount is the number of worker nodes; pass 0 for a single control-plane
// cluster. k8sVersion accepts "1.29", "1.29.2", or "v1.29.0".
//
// why: writing to ~/.kube/gossamerdb-<name>.yaml keeps the generated kubeconfig
// isolated from the user's default ~/.kube/config, preventing accidental
// context pollution across clusters.
func createKindCluster(name string, nodeCount int, k8sVersion string) (string, error) {
	provider := kindcluster.NewProvider()

	exists, err := alreadyExists(provider, name)
	if err != nil {
		return "", err
	}

	if !exists {
		kindCfg := &kindv1a4.Cluster{
			Nodes: buildNodes(nodeCount),
		}
		if err := provider.Create(
			name,
			kindcluster.CreateWithV1Alpha4Config(kindCfg),
			kindcluster.CreateWithNodeImage(k8sNodeImage(k8sVersion)),
		); err != nil {
			return "", fmt.Errorf("kind create: %w", err)
		}
	}

	kubeConfigStr, err := provider.KubeConfig(name, false)
	if err != nil {
		return "", fmt.Errorf("get kubeconfig: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	kubeconfigPath := filepath.Join(homeDir, ".kube", "gossamerdb-"+name+".yaml")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir kubeconfig dir: %w", err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte(kubeConfigStr), 0o600); err != nil {
		return "", fmt.Errorf("write kubeconfig: %w", err)
	}

	return kubeconfigPath, nil
}

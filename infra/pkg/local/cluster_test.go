package local

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestBuildNodes_controlPlaneOnly(t *testing.T) {
	nodes := buildNodes(0)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Role != kindv1a4.ControlPlaneRole {
		t.Errorf("node[0].Role = %q, want ControlPlaneRole", nodes[0].Role)
	}
}

func TestBuildNodes_withWorkers(t *testing.T) {
	nodes := buildNodes(3)
	if len(nodes) != 4 {
		t.Fatalf("want 4 nodes, got %d", len(nodes))
	}
	if nodes[0].Role != kindv1a4.ControlPlaneRole {
		t.Errorf("node[0].Role = %q, want ControlPlaneRole", nodes[0].Role)
	}
	for i, n := range nodes[1:] {
		if n.Role != kindv1a4.WorkerRole {
			t.Errorf("node[%d].Role = %q, want WorkerRole", i+1, n.Role)
		}
	}
}

func TestBuildNodes_negative(t *testing.T) {
	nodes := buildNodes(-1)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node for negative count, got %d", len(nodes))
	}
	if nodes[0].Role != kindv1a4.ControlPlaneRole {
		t.Errorf("node[0].Role = %q, want ControlPlaneRole", nodes[0].Role)
	}
}

func TestK8sNodeImage_bareMinor(t *testing.T) {
	got := k8sNodeImage("1.29")
	want := "kindest/node:v1.29.0"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "1.29", got, want)
	}
}

func TestK8sNodeImage_patch(t *testing.T) {
	got := k8sNodeImage("1.29.2")
	want := "kindest/node:v1.29.2"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "1.29.2", got, want)
	}
}

func TestK8sNodeImage_vPrefix(t *testing.T) {
	got := k8sNodeImage("v1.29.0")
	want := "kindest/node:v1.29.0"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "v1.29.0", got, want)
	}
}

func TestNewCluster(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH — skipping kind integration test")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not running — skipping kind integration test")
	}

	name := "gossamerdb-test-ci"
	t.Cleanup(func() {
		_ = exec.Command("kind", "delete", "cluster", "--name", name).Run()
		if homeDir, err := os.UserHomeDir(); err == nil {
			_ = os.Remove(filepath.Join(homeDir, ".kube", "gossamerdb-"+name+".yaml"))
		}
	})

	kubeconfigPath, err := createKindCluster(name, 0, "1.29")
	if err != nil {
		t.Fatalf("createKindCluster: %v", err)
	}

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("kubeconfig not written to %s: %v", kubeconfigPath, err)
	}
	if !strings.Contains(string(data), name) {
		t.Errorf("kubeconfig at %s does not mention cluster name %q", kubeconfigPath, name)
	}

	kubeconfigPath2, err := createKindCluster(name, 0, "1.29")
	if err != nil {
		t.Fatalf("second createKindCluster (idempotency): %v", err)
	}
	data2, err := os.ReadFile(kubeconfigPath2)
	if err != nil {
		t.Fatalf("kubeconfig missing after idempotent call: %v", err)
	}
	if !strings.Contains(string(data2), name) {
		t.Errorf("kubeconfig after idempotent call does not mention %q", name)
	}
}

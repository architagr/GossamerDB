// Package config defines the deployment configuration types for GossamerDB
// infrastructure. It is responsible for describing target environments and
// validating that a Config value is well-formed before it is passed to any
// Pulumi stack. It does not perform I/O or read from environment variables.
package config

import "fmt"

// Env identifies the target deployment environment.
type Env string

const (
	// EnvLocal targets a local development cluster (e.g. kind or minikube).
	EnvLocal Env = "local"
	// EnvK8s targets a generic Kubernetes cluster (non-cloud-managed).
	EnvK8s Env = "k8s"
	// EnvAWS targets an AWS EKS cluster.
	EnvAWS Env = "aws"
)

// Config holds the parameters that describe a GossamerDB deployment. Every
// field is optional except Env and ClusterName; call Validate before passing
// a Config to any stack constructor.
type Config struct {
	// Env is the deployment target. Must be one of EnvLocal, EnvK8s, EnvAWS.
	Env Env
	// ClusterName is the human-readable name for the cluster. Must be non-empty.
	ClusterName string
	// K8sVersion is the Kubernetes version string (e.g. "1.29"). Optional.
	K8sVersion string
	// NodeCount is the desired number of worker nodes. Zero means use the
	// stack default.
	NodeCount int
	// AWSRegion is the AWS region (e.g. "us-east-1"). Required only when
	// Env == EnvAWS.
	AWSRegion string
	// Namespace is the Kubernetes namespace for GossamerDB workloads.
	// Defaults to the stack default when empty.
	Namespace string
}

// Validate returns a non-nil error if c is missing required fields, specifies
// an unrecognised Env, or violates a cross-field constraint (e.g. AWSRegion
// must be non-empty when Env is EnvAWS).
func (c Config) Validate() error {
	if c.ClusterName == "" {
		return fmt.Errorf("clusterName must not be empty")
	}
	switch c.Env {
	case EnvLocal, EnvK8s:
		return nil
	case EnvAWS:
		if c.AWSRegion == "" {
			return fmt.Errorf("awsRegion must not be empty when env is aws")
		}
		return nil
	default:
		return fmt.Errorf("unknown env %q: must be local|k8s|aws", c.Env)
	}
}

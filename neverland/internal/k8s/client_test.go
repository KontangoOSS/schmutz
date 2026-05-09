package k8s_test

import (
	"testing"

	"git.konoss.org/kore/schmutz/neverland/internal/k8s"
)

func TestNewClient_MissingKubeconfigReturnsError(t *testing.T) {
	// With no in-cluster env and a non-existent kubeconfig, NewClient must return an error.
	_, err := k8s.NewClient("/tmp/this-file-does-not-exist-neverland.yaml")
	if err == nil {
		t.Fatal("expected error for missing kubeconfig, got nil")
	}
}

func TestNewClient_EmptyPathTriesInCluster(t *testing.T) {
	// Outside a cluster, in-cluster config will fail — that's expected.
	// We just confirm the function returns an error rather than panicking.
	_, err := k8s.NewClient("")
	if err == nil {
		t.Log("in-cluster config succeeded (running inside a cluster)")
	} else {
		t.Logf("in-cluster config failed as expected outside cluster: %v", err)
	}
}

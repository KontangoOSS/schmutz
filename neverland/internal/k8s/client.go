package k8s

import (
	rufio "github.com/tinkerbell/rufio/api/v1alpha1"
	tinkv1 "github.com/tinkerbell/tink/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewClient returns a controller-runtime client with Tinkerbell + Rufio types registered.
// If kubeconfigPath is empty it tries in-cluster config.
func NewClient(kubeconfigPath string) (client.Client, error) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = tinkv1.AddToScheme(scheme)
	_ = rufio.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	var cfg *rest.Config
	var err error

	if kubeconfigPath == "" {
		cfg, err = rest.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	}
	if err != nil {
		return nil, err
	}

	return client.New(cfg, client.Options{Scheme: scheme})
}

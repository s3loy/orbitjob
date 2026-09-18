package operator

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func NewInCluster(cfg Config) (Controller, error) {
	c, err := rest.InClusterConfig()
	if err != nil {
		return Controller{}, err
	}
	k, err := kubernetes.NewForConfig(c)
	if err != nil {
		return Controller{}, err
	}
	d, err := dynamic.NewForConfig(c)
	if err != nil {
		return Controller{}, err
	}
	return Controller{Kubernetes: k, Dynamic: d, Config: cfg}, nil
}

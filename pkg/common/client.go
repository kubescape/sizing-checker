package common

import (
	"log"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func BuildKubeClient(kubeconfigPath string) (*kubernetes.Clientset, bool) {
	inCluster := true

	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Not running in a cluster, so fallback to local kubeconfig
		inCluster = false

		// If user did not provide --kubeconfig, fallback to ~/.kube/config
		if kubeconfigPath == "" {
			kubeconfigPath = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			log.Printf("Could not load in-cluster or local kubeconfig: %v", err)
			return nil, inCluster
		}
	}

	// Build the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Failed to create Kubernetes clientset: %v", err)
		return nil, inCluster
	}

	return clientset, inCluster
}

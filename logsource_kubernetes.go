package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/alecthomas/kingpin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// A KubernetesLogSource can read lines from Kubernetes pod logs.
type KubernetesLogSource struct {
	clientset     *kubernetes.Clientset
	namespace     string
	labelSelector string
	containerName string
	logStream     io.ReadCloser
	scanner       *bufio.Scanner
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewKubernetesLogSource creates a new log source that reads from Kubernetes pod logs.
func NewKubernetesLogSource(namespace, labelSelector, containerName, kubeconfigPath string) (*KubernetesLogSource, error) {
	var config *rest.Config
	var err error

	// Try in-cluster config first (when running inside Kubernetes)
	config, err = rest.InClusterConfig()
	if err != nil {
		// If in-cluster config fails, try to use local kubeconfig for development
		log.Printf("Failed to get in-cluster config, trying local kubeconfig: %v", err)
		
		// Use provided kubeconfig path or default location
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config from kubeconfig: %v", err)
		}
		if kubeconfigPath != "" {
			log.Printf("Using kubeconfig from: %s", kubeconfigPath)
		} else {
			log.Printf("Using default kubeconfig for development")
		}
	} else {
		log.Printf("Using in-cluster kubernetes config")
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &KubernetesLogSource{
		clientset:     clientset,
		namespace:     namespace,
		labelSelector: labelSelector,
		containerName: containerName,
		ctx:           ctx,
		cancel:        cancel,
	}, nil
}

// initLogStream initializes the log stream from the first available pod
func (s *KubernetesLogSource) initLogStream() error {
	if s.logStream != nil {
		return nil // Already initialized
	}

	// List pods matching the label selector
	pods, err := s.clientset.CoreV1().Pods(s.namespace).List(s.ctx, metav1.ListOptions{
		LabelSelector: s.labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found with label selector %s in namespace %s", s.labelSelector, s.namespace)
	}

	// Use the first running pod
	var targetPod *corev1.Pod
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			targetPod = &pod
			break
		}
	}

	if targetPod == nil {
		return fmt.Errorf("no running pods found with label selector %s in namespace %s", s.labelSelector, s.namespace)
	}

	// Set up log streaming options
	logOptions := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: int64Ptr(10), // Start with last 10 lines
		Container: s.containerName,
	}

	// If no container specified, use the first container
	if s.containerName == "" && len(targetPod.Spec.Containers) > 0 {
		logOptions.Container = targetPod.Spec.Containers[0].Name
	}

	// Create log stream
	req := s.clientset.CoreV1().Pods(s.namespace).GetLogs(targetPod.Name, logOptions)
	logStream, err := req.Stream(s.ctx)
	if err != nil {
		return fmt.Errorf("failed to create log stream for pod %s: %v", targetPod.Name, err)
	}

	s.logStream = logStream
	s.scanner = bufio.NewScanner(logStream)

	log.Printf("Reading log events from Kubernetes pod %s/%s (container: %s)", s.namespace, targetPod.Name, logOptions.Container)
	return nil
}

func (s *KubernetesLogSource) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.logStream != nil {
		return s.logStream.Close()
	}
	return nil
}

func (s *KubernetesLogSource) Path() string {
	return fmt.Sprintf("kubernetes://%s/%s", s.namespace, s.labelSelector)
}

func (s *KubernetesLogSource) Read(ctx context.Context) (string, error) {
	// Initialize log stream if not already done
	if err := s.initLogStream(); err != nil {
		return "", err
	}

	// Use a select to handle context cancellation
	done := make(chan bool, 1)
	var line string
	var err error

	go func() {
		if s.scanner.Scan() {
			line = s.scanner.Text()
			err = nil
		} else {
			err = s.scanner.Err()
			if err == nil {
				err = io.EOF
			}
		}
		done <- true
	}()

	select {
	case <-done:
		if err == io.EOF {
			// Stream ended, try to reinitialize after a delay
			s.logStream.Close()
			s.logStream = nil
			s.scanner = nil
			time.Sleep(5 * time.Second)
			return "", io.EOF
		}
		return line, err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	}
}

// kubernetesLogSourceFactory is a factory that can create Kubernetes log sources
// from command line flags.
type kubernetesLogSourceFactory struct {
	namespace     string
	labelSelector string
	containerName string
	kubeconfigPath string
}

func (f *kubernetesLogSourceFactory) Init(app *kingpin.Application) {
	app.Flag("kubernetes.namespace", "Kubernetes namespace to read logs from.").StringVar(&f.namespace)
	app.Flag("kubernetes.label-selector", "Label selector to find pods (e.g., app=postfix).").StringVar(&f.labelSelector)
	app.Flag("kubernetes.container", "Container name to read logs from (optional, uses first container if not specified).").StringVar(&f.containerName)
	app.Flag("kubernetes.kubeconfig", "Path to kubeconfig file for development (optional, uses ~/.kube/config if not specified).").StringVar(&f.kubeconfigPath)
}

func (f *kubernetesLogSourceFactory) New(ctx context.Context) (LogSourceCloser, error) {
	// Only create if both namespace and label selector are provided
	if f.namespace == "" || f.labelSelector == "" {
		return nil, nil
	}

	// Validate label selector format
	if !strings.Contains(f.labelSelector, "=") && !strings.Contains(f.labelSelector, " in ") {
		return nil, fmt.Errorf("invalid label selector format: %s (expected format: key=value or key in (value1,value2))", f.labelSelector)
	}

	return NewKubernetesLogSource(f.namespace, f.labelSelector, f.containerName, f.kubeconfigPath)
}

// Helper function to create int64 pointer
func int64Ptr(i int64) *int64 {
	return &i
}

func init() {
	RegisterLogSourceFactory(&kubernetesLogSourceFactory{})
}

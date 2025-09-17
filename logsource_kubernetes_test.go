package main

import (
	"context"
	"os"
	"testing"

	"github.com/alecthomas/kingpin"
	"github.com/stretchr/testify/assert"
)

func TestKubernetesLogSourceFactory_Init(t *testing.T) {
	app := kingpin.New("test", "test")
	factory := &kubernetesLogSourceFactory{}

	factory.Init(app)

	// Parse some test flags
	args := []string{
		"--kubernetes.namespace", "default",
		"--kubernetes.label-selector", "app=postfix",
		"--kubernetes.container", "postfix",
		"--kubernetes.kubeconfig", "/path/to/kubeconfig",
	}

	_, err := app.Parse(args)
	if err != nil {
		t.Fatalf("Failed to parse args: %v", err)
	}

	assert.Equal(t, "default", factory.namespace)
	assert.Equal(t, "app=postfix", factory.labelSelector)
	assert.Equal(t, "postfix", factory.containerName)
	assert.Equal(t, "/path/to/kubeconfig", factory.kubeconfigPath)
}

func TestKubernetesLogSourceFactory_New_NoConfig(t *testing.T) {
	ctx := context.Background()
	factory := &kubernetesLogSourceFactory{}

	// Should return nil when not configured
	src, err := factory.New(ctx)
	assert.NoError(t, err)
	assert.Nil(t, src)
}

func TestKubernetesLogSourceFactory_New_InvalidLabelSelector(t *testing.T) {
	ctx := context.Background()
	factory := &kubernetesLogSourceFactory{
		namespace:     "default",
		labelSelector: "invalid-selector",
	}

	// Should return error for invalid label selector
	src, err := factory.New(ctx)
	assert.Error(t, err)
	assert.Nil(t, src)
	assert.Contains(t, err.Error(), "invalid label selector format")
}

func TestKubernetesLogSource_Path(t *testing.T) {
	source := &KubernetesLogSource{
		namespace:     "test-namespace",
		labelSelector: "app=test",
	}

	expected := "kubernetes://test-namespace/app=test"
	assert.Equal(t, expected, source.Path())
}

func TestKubernetesLogSourceFactory_EnvironmentVariables(t *testing.T) {
	// Test environment variable support
	os.Setenv("KUBERNETES_POD_NAME", "test-pod")
	os.Setenv("KUBERNETES_NAMESPACE", "test-namespace")
	defer os.Unsetenv("KUBERNETES_POD_NAME")
	defer os.Unsetenv("KUBERNETES_NAMESPACE")

	app := kingpin.New("test", "test")
	factory := &kubernetesLogSourceFactory{}
	factory.Init(app)

	args := []string{}
	_, err := app.Parse(args)
	if err != nil {
		t.Fatalf("Failed to parse args: %v", err)
	}

	// Test that environment variables were parsed correctly
	assert.Equal(t, "test-pod", factory.podName)
	assert.Equal(t, "test-namespace", factory.namespace)
}

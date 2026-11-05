package controller

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/net-pilgrim/kube-mdns/pkg/mdns"
)

func TestCRDControllerReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)

	mgr, err := mdns.NewManager("lo", "_http._tcp.local.")
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	ctrl := NewCRDController(dynClient, mgr, 80, time.Minute)

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mdns.net-pilgrim.dev/v1alpha1",
			"kind":       "MdnsRecord",
			"metadata": map[string]interface{}{
				"name":      "test-record",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"host":        "test.local",
				"ip":          "192.168.1.50",
				"port":        int64(8080),
				"serviceType": "_http._tcp.local.",
			},
		},
	}

	ctrl.reconcile(u)

	// Verify published record in manager
	entries := mgr.GetEntriesForHost("test.local.")
	if len(entries) == 0 {
		t.Errorf("Expected entry for test.local, got none")
	}

	// Now delete record
	key := "crd/default/test-record"
	mgr.Unpublish(key)

	entriesAfter := mgr.GetEntriesForHost("test.local.")
	if len(entriesAfter) != 0 {
		t.Errorf("Expected 0 entries after unpublish, got %d", len(entriesAfter))
	}
}

func TestCRDControllerRunGraceful(t *testing.T) {
	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClient(scheme)

	mgr, err := mdns.NewManager("lo", "_http._tcp.local.")
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	ctrl := NewCRDController(dynClient, mgr, 80, time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_ = ctrl.Run(ctx)
}

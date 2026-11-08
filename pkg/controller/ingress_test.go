package controller

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/net-pilgrim/kube-mdns/pkg/mdns"
)

func TestResolvePort(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	mdnsMgr, _ := mdns.NewManager("", "_http._tcp")
	ctrl := NewIngressController(fakeClient, mdnsMgr, 80, "", "", 0)

	// Case 1: Annotation port
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AnnPort: "8080",
			},
		},
	}
	if p := ctrl.resolvePort(ing, ing.Annotations); p != 8080 {
		t.Errorf("expected 8080 from annotation, got %d", p)
	}

	// Case 2: Status loadbalancer port
	port80 := int32(80)
	ingLB := &networkingv1.Ingress{
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{
					{
						IP: "192.168.1.100",
						Ports: []networkingv1.IngressPortStatus{
							{Port: port80},
						},
					},
				},
			},
		},
	}
	if p := ctrl.resolvePort(ingLB, nil); p != 80 {
		t.Errorf("expected 80 from loadbalancer, got %d", p)
	}

	// Case 3: TLS fallback to 443
	ingTLS := &networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{"test.local"}},
			},
		},
	}
	ctrlNoDefault := NewIngressController(fakeClient, mdnsMgr, 0, "", "", 0)
	if p := ctrlNoDefault.resolvePort(ingTLS, nil); p != 443 {
		t.Errorf("expected 443 from TLS spec, got %d", p)
	}
}

func TestIngressControllerReconcileWithIcon(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	mgr, err := mdns.NewManager("lo", "_http._tcp.local.")
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Shutdown()

	ctrl := NewIngressController(fakeClient, mgr, 80, "RackMac", "", 0)

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dashboard",
			Namespace: "default",
			Annotations: map[string]string{
				AnnIcon:         "Macmini",
				AnnTXT:          "path=/,env=prod",
				AnnPrefix + "/txt-version": "1.0.0",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{Host: "dashboard.local"},
			},
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{
					{IP: "192.168.1.150"},
				},
			},
		},
	}

	ctrl.reconcile(ing)

	// Check entries
	entries := mgr.GetEntriesForHost("dashboard.local.")
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries (primary + device-info), got %d", len(entries))
	}

	// Check unpublish on delete
	key := "default/dashboard"
	mgr.Unpublish(key)
	if len(mgr.GetEntriesForHost("dashboard.local.")) != 0 {
		t.Errorf("expected 0 entries after unpublish")
	}
}

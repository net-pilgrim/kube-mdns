package controller

import (
	"context"
	"fmt"
	"log"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/net-pilgrim/kube-mdns/pkg/mdns"
)

var MdnsRecordGVR = schema.GroupVersionResource{
	Group:    "mdns.net-pilgrim.dev",
	Version:  "v1alpha1",
	Resource: "mdnsrecords",
}

type CRDController struct {
	dynamicClient dynamic.Interface
	mdnsManager   *mdns.Manager
	defaultPort   int
	informer      cache.SharedIndexInformer
}

func NewCRDController(
	dynamicClient dynamic.Interface,
	mdnsManager *mdns.Manager,
	defaultPort int,
	resyncPeriod time.Duration,
) *CRDController {
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, resyncPeriod)
	informer := factory.ForResource(MdnsRecordGVR).Informer()

	c := &CRDController{
		dynamicClient: dynamicClient,
		mdnsManager:   mdnsManager,
		defaultPort:   defaultPort,
		informer:      informer,
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return
			}
			c.reconcile(u)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			u, ok := newObj.(*unstructured.Unstructured)
			if !ok {
				return
			}
			c.reconcile(u)
		},
		DeleteFunc: func(obj interface{}) {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				u, ok = tombstone.Obj.(*unstructured.Unstructured)
				if !ok {
					return
				}
			}
			key := fmt.Sprintf("crd/%s/%s", u.GetNamespace(), u.GetName())
			c.mdnsManager.Unpublish(key)
		},
	})

	return c
}

func (c *CRDController) Run(ctx context.Context) error {
	log.Printf("Starting Kubernetes MdnsRecord CRD Controller watcher...")
	go c.informer.Run(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced) {
		return fmt.Errorf("timed out waiting for MdnsRecord informer cache to sync")
	}

	log.Printf("MdnsRecord Controller cache synced. Watching for MdnsRecord changes...")
	<-ctx.Done()
	log.Printf("Shutting down MdnsRecord Controller...")
	return nil
}

func (c *CRDController) reconcile(u *unstructured.Unstructured) {
	key := fmt.Sprintf("crd/%s/%s", u.GetNamespace(), u.GetName())

	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		c.mdnsManager.Unpublish(key)
		return
	}

	host, _ := spec["host"].(string)
	ip, _ := spec["ip"].(string)
	if host == "" || ip == "" {
		c.mdnsManager.Unpublish(key)
		return
	}

	port := c.defaultPort
	if port <= 0 {
		port = 80
	}
	if p, ok := spec["port"].(int64); ok && p > 0 {
		port = int(p)
	} else if p, ok := spec["port"].(float64); ok && p > 0 {
		port = int(p)
	} else if p, ok := spec["port"].(int); ok && p > 0 {
		port = p
	}

	serviceType, _ := spec["serviceType"].(string)

	record := mdns.Record{
		Hostname:    host,
		IPs:         []string{ip},
		Port:        port,
		ServiceType: serviceType,
	}

	c.mdnsManager.Publish(key, []mdns.Record{record})
}

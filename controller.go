package main

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var PREFIX_SVC = "external-"

type reconcileSVC struct {
	client client.Client
	log    logr.Logger
}

func (r *reconcileSVC) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch all svc from cache
	services := &corev1.ServiceList{}
	err := r.client.List(ctx, services, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{"longhorn.io/managed-by": "longhorn-manager"}),
	})
	if err != nil {
		r.log.Error(err, "Unable to fetch Services")
		return reconcile.Result{}, err
	}

	for _, svc := range services.Items {
		labels := svc.GetLabels()

		// Check if the external-share service has already been created
		lbsvc := &corev1.Service{}
		lbsvcName := PREFIX_SVC + svc.Name
		err = r.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: lbsvcName}, lbsvc)
		if err == nil {
			// Skip (already exists)
			continue
		}

		//　Consider share-manager name as PV name
		if pvname, ok := labels["longhorn.io/share-manager"]; ok {

			pv := &corev1.PersistentVolume{}
			err := r.client.Get(ctx, client.ObjectKey{Namespace: corev1.NamespaceAll, Name: pvname}, pv)
			if err != nil {
				// The error occurs even if the PV was deleted before the Service.
				// Therefore, output a message in Info.
				r.log.Info("Unable to get pv: " + pvname)
				continue
			}

			pvc := &corev1.PersistentVolumeClaim{}
			err = r.client.Get(ctx, client.ObjectKey{Namespace: pv.Spec.ClaimRef.Namespace, Name: pv.Spec.ClaimRef.Name}, pvc)
			if err != nil {
				// The error occurs even if the PVC was deleted before the Service.
				// Therefore, output a message in Info.
				r.log.Info("Unable to get pvc: " + pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name)
				continue
			}

			externalSahre := "false"
			if externalSahre, ok = pvc.Annotations["longhorn.external.share"]; ok {
				if strings.EqualFold(externalSahre, "true") {
					r.log.Info("Creating new TypeLoadBalancer's service. external-" + pvname)
					newlb := createLBObject(svc, *pv)
					err = r.client.Create(ctx, &newlb)
					if err != nil {
						r.log.Error(err, "Can't create TypeLoadBalancer's Service")
					}
				}
			}
		}
	}

	return reconcile.Result{}, nil
}

func createLBObject(svc corev1.Service, pv corev1.PersistentVolume) corev1.Service {
	labels := map[string]string{
		"external.share/pv":         pv.Name,
		"external.share/managed-by": "longhorn-external-share-manager",
	}
	ownerreference := v1.OwnerReference{
		APIVersion: pv.APIVersion,
		Kind:       pv.Kind,
		Name:       pv.Name,
		UID:        pv.UID,
	}

	selector := map[string]string{
		"longhorn.io/share-manager": svc.Name,
	}
	ports := corev1.ServicePort{
		Name: "nfs",
		Port: 2049,
	}

	newsvc := corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      PREFIX_SVC + svc.Name,
			Namespace: svc.Namespace,
			Labels:    labels,
			OwnerReferences: []v1.OwnerReference{
				ownerreference,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:     "LoadBalancer",
			Selector: selector,
			Ports: []corev1.ServicePort{
				ports,
			},
		},
	}
	return newsvc
}

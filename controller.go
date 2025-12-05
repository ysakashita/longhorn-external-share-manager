package main

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var PREFIX_SVC = "external-"

type reconcileSVC struct {
	client client.Client
	log    logr.Logger
}

func (r *reconcileSVC) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the specific service that triggered this reconciliation
	svc := &corev1.Service{}
	err := r.client.Get(ctx, request.NamespacedName, svc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Service was deleted, nothing to do (external service will be cleaned by owner reference)
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Unable to fetch Service", "name", request.Name, "namespace", request.Namespace)
		return reconcile.Result{}, err
	}

	// Only process services managed by Longhorn
	labels := svc.GetLabels()
	if labels["longhorn.io/managed-by"] != "longhorn-manager" {
		// Not a Longhorn service, ignore
		return reconcile.Result{}, nil
	}

	// Process this service
	r.processService(ctx, *svc)
	return reconcile.Result{}, nil
}

func (r *reconcileSVC) processService(ctx context.Context, svc corev1.Service) {
	labels := svc.GetLabels()

	// Check if the external-share service has already been created
	lbsvc := &corev1.Service{}
	lbsvcName := PREFIX_SVC + svc.Name
	err := r.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: lbsvcName}, lbsvc)
	if err != nil && !apierrors.IsNotFound(err) {
		// Unexpected error
		r.log.Error(err, "Unable to get LoadBalancer service", "name", lbsvcName)
		return
	}
	if err == nil {
		// Service already exists, check if it should be deleted
		// Check if annotation still exists and is true
		shouldDelete := false
		if pvname, ok := labels["longhorn.io/share-manager"]; ok {
			pv := &corev1.PersistentVolume{}
			err := r.client.Get(ctx, client.ObjectKey{Namespace: corev1.NamespaceAll, Name: pvname}, pv)
			if apierrors.IsNotFound(err) {
				// PV deleted, service will be cleaned by owner reference
				return
			}
			if err != nil {
				r.log.Error(err, "Unable to get pv", "name", pvname)
				return
			}

			if pv.Spec.ClaimRef == nil {
				r.log.Info("PV has no ClaimRef, skipping", "pv", pvname)
				return
			}

			pvc := &corev1.PersistentVolumeClaim{}
			err = r.client.Get(ctx, client.ObjectKey{Namespace: pv.Spec.ClaimRef.Namespace, Name: pv.Spec.ClaimRef.Name}, pvc)
			if apierrors.IsNotFound(err) {
				// PVC deleted, service will be cleaned by owner reference
				return
			}
			if err != nil {
				r.log.Error(err, "Unable to get pvc", "namespace", pv.Spec.ClaimRef.Namespace, "name", pv.Spec.ClaimRef.Name)
				return
			}

			// Check if annotation is missing or not "true"
			externalShare, ok := pvc.Annotations["longhorn.external.share"]
			if !ok || !strings.EqualFold(externalShare, "true") {
				shouldDelete = true
			}
		}

		if shouldDelete {
			r.log.Info("Deleting external LoadBalancer service (annotation removed)", "name", lbsvcName)
			err = r.client.Delete(ctx, lbsvc)
			if err != nil && !apierrors.IsNotFound(err) {
				r.log.Error(err, "Failed to delete LoadBalancer service", "name", lbsvcName)
			}
		}
		return
	}

	// Consider share-manager name as PV name
	if pvname, ok := labels["longhorn.io/share-manager"]; ok {

		pv := &corev1.PersistentVolume{}
		err := r.client.Get(ctx, client.ObjectKey{Namespace: corev1.NamespaceAll, Name: pvname}, pv)
		if err != nil {
			if apierrors.IsNotFound(err) {
				r.log.Info("PV not found (may have been deleted)", "pv", pvname)
			} else {
				r.log.Error(err, "Unable to get pv", "name", pvname)
			}
			return
		}

		if pv.Spec.ClaimRef == nil {
			r.log.Info("PV has no ClaimRef, skipping", "pv", pvname)
			return
		}

		pvc := &corev1.PersistentVolumeClaim{}
		err = r.client.Get(ctx, client.ObjectKey{Namespace: pv.Spec.ClaimRef.Namespace, Name: pv.Spec.ClaimRef.Name}, pvc)
		if err != nil {
			if apierrors.IsNotFound(err) {
				r.log.Info("PVC not found (may have been deleted)", "namespace", pv.Spec.ClaimRef.Namespace, "name", pv.Spec.ClaimRef.Name)
			} else {
				r.log.Error(err, "Unable to get pvc", "namespace", pv.Spec.ClaimRef.Namespace, "name", pv.Spec.ClaimRef.Name)
			}
			return
		}

		externalShare := ""
		if externalShare, ok = pvc.Annotations["longhorn.external.share"]; ok {
			if strings.EqualFold(externalShare, "true") {
				r.log.Info("Creating new TypeLoadBalancer's service", "name", "external-"+pvname)
				newlb := createLBObject(svc, *pv)
				err = r.client.Create(ctx, &newlb)
				if err != nil {
					r.log.Error(err, "Can't create TypeLoadBalancer's Service")
				}
			}
		}
	}
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

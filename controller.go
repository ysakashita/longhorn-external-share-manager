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
	// Consider share-manager name as PV name
	pvname, ok := svc.GetLabels()["longhorn.io/share-manager"]
	if !ok {
		return
	}

	pv, ok := r.getPV(ctx, pvname)
	if !ok {
		return
	}

	pvc, ok := r.getPVC(ctx, *pv)
	if !ok {
		return
	}

	nfsEnabled := strings.EqualFold(pvc.Annotations["longhorn.external.share"], "true")
	smbEnabled := strings.EqualFold(pvc.Annotations[smbAnnotation], "true")
	smbAuthEnabled := strings.EqualFold(pvc.Annotations[smbAuthAnnotation], "true")

	r.reconcileNFSLoadBalancer(ctx, svc, *pv, nfsEnabled)
	r.reconcileSMBGateway(ctx, svc, *pv, smbEnabled, smbAuthEnabled)
}

// getPV fetches the PersistentVolume backing a share-manager Service. It
// logs and returns ok=false if the PV can't be found or fetched.
func (r *reconcileSVC) getPV(ctx context.Context, name string) (*corev1.PersistentVolume, bool) {
	pv := &corev1.PersistentVolume{}
	err := r.client.Get(ctx, client.ObjectKey{Name: name}, pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.log.Info("PV not found (may have been deleted)", "pv", name)
		} else {
			r.log.Error(err, "Unable to get pv", "name", name)
		}
		return nil, false
	}
	return pv, true
}

// getPVC fetches the PersistentVolumeClaim claiming the given PersistentVolume.
// It logs and returns ok=false if the PV has no ClaimRef or the PVC can't be
// found or fetched.
func (r *reconcileSVC) getPVC(ctx context.Context, pv corev1.PersistentVolume) (*corev1.PersistentVolumeClaim, bool) {
	if pv.Spec.ClaimRef == nil {
		r.log.Info("PV has no ClaimRef, skipping", "pv", pv.Name)
		return nil, false
	}

	pvc := &corev1.PersistentVolumeClaim{}
	err := r.client.Get(ctx, client.ObjectKey{Namespace: pv.Spec.ClaimRef.Namespace, Name: pv.Spec.ClaimRef.Name}, pvc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.log.Info("PVC not found (may have been deleted)", "namespace", pv.Spec.ClaimRef.Namespace, "name", pv.Spec.ClaimRef.Name)
		} else {
			r.log.Error(err, "Unable to get pvc", "namespace", pv.Spec.ClaimRef.Namespace, "name", pv.Spec.ClaimRef.Name)
		}
		return nil, false
	}
	return pvc, true
}

// reconcileNFSLoadBalancer creates (or deletes) the LoadBalancer Service
// that exposes the share-manager pod's NFS server directly to clients
// outside the cluster.
func (r *reconcileSVC) reconcileNFSLoadBalancer(ctx context.Context, svc corev1.Service, pv corev1.PersistentVolume, enabled bool) {
	lbsvcName := PREFIX_SVC + svc.Name

	lbsvc := &corev1.Service{}
	err := r.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: lbsvcName}, lbsvc)
	if err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Unable to get LoadBalancer service", "name", lbsvcName)
		return
	}

	if err == nil {
		// Service already exists; delete it if it's no longer wanted.
		if !enabled {
			r.log.Info("Deleting external LoadBalancer service (annotation removed)", "name", lbsvcName)
			if err := r.client.Delete(ctx, lbsvc); err != nil && !apierrors.IsNotFound(err) {
				r.log.Error(err, "Failed to delete LoadBalancer service", "name", lbsvcName)
			}
		}
		return
	}

	// Service doesn't exist yet; create it if wanted.
	if enabled {
		r.log.Info("Creating new TypeLoadBalancer's service", "name", lbsvcName)
		newlb := createLBObject(svc, pv)
		if err := r.client.Create(ctx, &newlb); err != nil {
			r.log.Error(err, "Can't create TypeLoadBalancer's Service")
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

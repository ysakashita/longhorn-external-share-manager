package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	smbAnnotation     = "longhorn.external.share.smb"
	smbAuthAnnotation = "longhorn.external.share.smb.auth"
	smbPrefix         = "smb-"
	smbGatewayImage   = "ysakashita/longhorn-external-share-manager-smb-gateway:v0.1.0"
	smbShareName      = "share"
	smbSecretUser     = "longhorn"
	smbMountPath      = "/export"
)

// reconcileSMBGateway creates (or deletes) the NFS-to-SMB gateway resources
// (optionally a Secret, a Deployment, a LoadBalancer Service) for a Longhorn
// RWX volume, so it can be mounted natively from iOS/iPadOS clients, which
// only support SMB. The gateway Pod mounts the existing NFS export via a
// plain Kubernetes NFS volume (kubelet performs the mount on the node), so
// no privileged container or in-container NFS client is required.
//
// By default the SMB share is guest-accessible (no authentication), mirroring
// this project's existing stance that the plain NFS export has no access
// control. Passing authEnabled=true switches the share to require a
// generated username/password, stored in a Secret.
func (r *reconcileSVC) reconcileSMBGateway(ctx context.Context, svc corev1.Service, pv corev1.PersistentVolume, enabled, authEnabled bool) {
	name := smbPrefix + svc.Name

	if !enabled {
		r.deleteSMBGateway(ctx, svc.Namespace, name)
		return
	}

	ownerreference := v1.OwnerReference{
		APIVersion: pv.APIVersion,
		Kind:       pv.Kind,
		Name:       pv.Name,
		UID:        pv.UID,
	}
	labels := map[string]string{
		"external.share/pv":         pv.Name,
		"external.share/managed-by": "longhorn-external-share-manager",
		"external.share/role":       "smb-gateway",
	}

	var secretName string
	if authEnabled {
		secret, err := r.ensureSMBSecret(ctx, svc.Namespace, name, ownerreference, labels)
		if err != nil {
			r.log.Error(err, "Unable to ensure SMB gateway credentials Secret", "name", name)
			return
		}
		secretName = secret.Name
	}

	deploy := &appsv1.Deployment{}
	err := r.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: name}, deploy)
	if err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Unable to get SMB gateway Deployment", "name", name)
		return
	}
	if apierrors.IsNotFound(err) {
		newDeploy := createSMBDeploymentObject(name, svc, pv, secretName, ownerreference, labels)
		if err := r.client.Create(ctx, &newDeploy); err != nil {
			r.log.Error(err, "Can't create SMB gateway Deployment", "name", name)
			return
		}
		r.log.Info("Created SMB gateway Deployment", "name", name, "authEnabled", authEnabled)
	}

	lbsvc := &corev1.Service{}
	err = r.client.Get(ctx, client.ObjectKey{Namespace: svc.Namespace, Name: name}, lbsvc)
	if err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Unable to get SMB gateway Service", "name", name)
		return
	}
	if apierrors.IsNotFound(err) {
		newSvc := createSMBServiceObject(name, svc.Namespace, ownerreference, labels)
		if err := r.client.Create(ctx, &newSvc); err != nil {
			r.log.Error(err, "Can't create SMB gateway Service", "name", name)
			return
		}
		r.log.Info("Created SMB gateway Service", "name", name)
	}
}

func (r *reconcileSVC) deleteSMBGateway(ctx context.Context, namespace, name string) {
	lbsvc := &corev1.Service{ObjectMeta: v1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := r.client.Delete(ctx, lbsvc); err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Failed to delete SMB gateway Service", "name", name)
	}

	deploy := &appsv1.Deployment{ObjectMeta: v1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := r.client.Delete(ctx, deploy); err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Failed to delete SMB gateway Deployment", "name", name)
	}

	secret := &corev1.Secret{ObjectMeta: v1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := r.client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		r.log.Error(err, "Failed to delete SMB gateway Secret", "name", name)
	}
}

// ensureSMBSecret returns the existing credentials Secret for the gateway,
// or creates one with a freshly generated random password. Once created, it
// is never modified by later reconciles.
func (r *reconcileSVC) ensureSMBSecret(ctx context.Context, namespace, name string, owner v1.OwnerReference, labels map[string]string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, secret)
	if err == nil {
		return secret, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}

	password, err := generateRandomPassword(20)
	if err != nil {
		return nil, err
	}

	immutable := true
	newSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			OwnerReferences: []v1.OwnerReference{owner},
		},
		Type:      corev1.SecretTypeOpaque,
		Immutable: &immutable,
		StringData: map[string]string{
			"username": smbSecretUser,
			"password": password,
		},
	}
	if err := r.client.Create(ctx, newSecret); err != nil {
		return nil, err
	}
	r.log.Info("Created SMB gateway credentials Secret", "name", name)
	return newSecret, nil
}

func generateRandomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func createSMBDeploymentObject(name string, svc corev1.Service, pv corev1.PersistentVolume, secretName string, owner v1.OwnerReference, labels map[string]string) appsv1.Deployment {
	replicas := int32(1)

	env := []corev1.EnvVar{
		{Name: "SMB_SHARE_PATH", Value: smbMountPath},
		{Name: "SMB_SHARE_NAME", Value: smbShareName},
	}
	if secretName != "" {
		env = append(env,
			corev1.EnvVar{
				Name: "SMB_USER",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "username",
					},
				},
			},
			corev1.EnvVar{
				Name: "SMB_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  "password",
					},
				},
			},
		)
	}

	return appsv1.Deployment{
		ObjectMeta: v1.ObjectMeta{
			Name:            name,
			Namespace:       svc.Namespace,
			Labels:          labels,
			OwnerReferences: []v1.OwnerReference{owner},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &v1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: v1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					// The NFS export is mounted by kubelet on the node via
					// the standard NFS volume plugin -- no privileged
					// container or in-container NFS client is required.
					Volumes: []corev1.Volume{
						{
							Name: "nfs-export",
							VolumeSource: corev1.VolumeSource{
								NFS: &corev1.NFSVolumeSource{
									Server: svc.Spec.ClusterIP,
									Path:   "/" + pv.Name,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "smb-gateway",
							Image: smbGatewayImage,
							Env:   env,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "nfs-export", MountPath: smbMountPath},
							},
							Ports: []corev1.ContainerPort{
								{Name: "smb", ContainerPort: 445},
							},
						},
					},
				},
			},
		},
	}
}

func createSMBServiceObject(name, namespace string, owner v1.OwnerReference, labels map[string]string) corev1.Service {
	return corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			OwnerReferences: []v1.OwnerReference{owner},
		},
		Spec: corev1.ServiceSpec{
			Type:     "LoadBalancer",
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "smb", Port: 445},
			},
		},
	}
}

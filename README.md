# Longhorn external share manager

The Longhorn external share manager allows Longhorn's volumes on Kubernetes to be accessed from outside Kubernetes(e.g., VM or Baremetal Server etc.).

[Longhorn](https://longhorn.io/) creates a pod (Share manager) as an NFS server when it creates a volume of ReadWriteMany in Access Mode as a PersistentVolume(see. [Longhorn docs/ReadWriteMany(RWX) Volume](https://longhorn.io/docs/1.5.3/advanced-resources/rwx-workloads/)).

This Longhorn external share manager automatically creates a Service set to `Type: LoadBalancer` that connects to this NFS server pod.

# Usage

1. Create PVC(PersistentVolumeClaim) with an annotation(`longhorn.external.share: "true"`)

(e.g., `nfs-volume.yaml`)

```YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nfs-longhorn
  annotations: 
    longhorn.external.share: "true" # The annotation indicates the target of the longhorn external share manager
spec:
  storageClassName: longhorn
  accessModes:
    - ReadWriteMany # Must be set to ReadWriteMany for NFS
  resources:
    requests:
      storage: 10Gi
```

2. Deploy the PVC manifest

(e.g.,)
```
$ kubectl apply -f nfs-volume.yaml
```

3. Check the service(SVC) which generated automatically

The SVC name to be created is `external-<PV name>`.

(e.g.,)

```
$ kubectl get svc -n longhorn-system
```


4. Mount the volume from client outside Kubernetes

(e.g.,)

```
$ sudo mount -t nfs -o vers=4.2 192.168.0.121:/pvc-xxxx /mnt/lhvol1
```

:memo: 
Longhorn's NFS server version is 4.2.
When mounting a volume (Step 4), use a Client that supports NFS v4.2.

:memo: 
The auto-generated services are deleted when the target PV (Persistent Volume) is deleted, as well as when annotation is changed.

:memo: 
If you want to stop publishing outside Kubernetes, please delete the auto-generated SVC after removing the annotation(`longhorn.external.share`) in PVC.

# Setup

You deploy manifests for Longhorn external manager.

```
$ kubectl apply -k manifests/
```

:memo:
The namespace to deploy to is `longhorn-system`.
If you want to change the namespace, please change the manifests.

# Notice

An access control is not configured on Longhorn's NFS Server.
Therefore, volumes published by the Longhorn external manager can be accessed from any client.
For production use, please use with caution.

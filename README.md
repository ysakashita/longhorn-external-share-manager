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
Longhorn's NFS server (Ganesha) supports NFSv4.0, 4.1 and 4.2.
When mounting a volume (Step 4), use a Client that supports one of these versions.

:memo:
macOS's NFS client does not support NFSv4.2 (see `man mount_nfs`: "Currently NFSv4 is the highest supported version with a minor version of zero or one"). From macOS, mount with `vers=4.1` instead:
```
$ sudo mount -t nfs -o vers=4.1 192.168.0.121:/pvc-xxxx /mnt/lhvol1
```

:memo: 
The auto-generated services are deleted when the target PV (Persistent Volume) is deleted, as well as when annotation is changed.

:memo: 
If you want to stop publishing outside Kubernetes, please delete the auto-generated SVC after removing the annotation(`longhorn.external.share`) in PVC.

# Mounting from iPad/iPhone/Windows (SMB)

iOS/iPadOS's Files app has no native NFS client — Apple only ships an SMB
client. Windows can mount NFS too, but it requires enabling an optional
Windows feature ("Client for NFS", not available on Windows Home editions)
and doesn't integrate with File Explorer as smoothly as SMB does. To mount a
volume from an iPad, iPhone, or Windows PC, add a second annotation to the
PVC to opt into an NFS-to-SMB gateway for that volume:

```YAML
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nfs-longhorn
  annotations:
    longhorn.external.share: "true"
    longhorn.external.share.smb: "true" # Opts this volume into the SMB gateway
spec:
  storageClassName: longhorn
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 10Gi
```

`longhorn.external.share.smb` works independently of `longhorn.external.share`
— the SMB gateway talks to Longhorn's share-manager over its internal
ClusterIP Service, so it doesn't need the plain NFS `LoadBalancer` Service to
exist.

This creates a `LoadBalancer` Service named `smb-<share-manager SVC name>`
on port 445. Each volume that opts in gets its **own separate Service and
its own IP** — the gateway is per-volume, just like the plain NFS
`LoadBalancer` Service. Find it the same way as the NFS one:

```
$ kubectl get svc -n longhorn-system
```

:memo:
The SMB share name inside each gateway is always `share` (fixed) — it does
**not** identify the volume. What distinguishes one volume's mount from
another's is the `<LoadBalancer IP>` itself, i.e. which `smb-*` Service's IP
you connect to. Match the `smb-<share-manager SVC name>` Service to the PV
you care about before mounting.

From the iPad/iPhone Files app: **Browse** → **⋯** → **Connect to Server** →
`smb://<LoadBalancer IP>/share`.

From Windows, either open File Explorer and enter `\\<LoadBalancer IP>\share`
in the address bar, or from a command prompt:

```
> net use Z: \\<LoadBalancer IP>\share
```

:memo:
By default the SMB share has **no authentication** (guest access), matching
this project's existing stance that the plain NFS export also has no access
control (see the Notice section below). To require a username and password
instead, add `longhorn.external.share.smb.auth: "true"` to the PVC
annotations. A random password is generated once and stored in a Secret
named `smb-<share-manager SVC name>` in the same namespace as the PVC:

```
$ kubectl get secret -n longhorn-system smb-<share-manager SVC name> -o jsonpath='{.data.username}' | base64 -d
$ kubectl get secret -n longhorn-system smb-<share-manager SVC name> -o jsonpath='{.data.password}' | base64 -d
```

On Windows, pass the credentials to `net use` directly:

```
> net use Z: \\<LoadBalancer IP>\share <password> /user:<username>
```

:memo:
Changing `longhorn.external.share.smb.auth` on a volume that already has an
SMB gateway does **not** update it in place — this controller only creates
missing resources and deletes ones that are no longer wanted, it never
patches existing ones. To change the authentication mode, set
`longhorn.external.share.smb` to `"false"` first (which deletes the gateway
and its Secret), then set it back to `"true"` with the new
`longhorn.external.share.smb.auth` value.

:memo:
Like the auto-generated NFS `LoadBalancer` Service, the SMB gateway's
Service, Deployment, and (if authentication is enabled) Secret are deleted
automatically when the PV is deleted or the `longhorn.external.share.smb`
annotation is removed.

# Setup

You deploy manifests for Longhorn external manager.

```
$ kubectl apply -f manifests/
```

:memo:
The namespace to deploy to is `longhorn-system`.
If you want to change the namespace, please change the manifests.

# Notice

An access control is not configured on Longhorn's NFS Server.
Therefore, volumes published by the Longhorn external manager can be accessed from any client.
For production use, please use with caution.

:memo:
The SMB gateway is guest-accessible (no authentication) by default, matching
the NFS behavior above. Enabling `longhorn.external.share.smb.auth` narrows
this to credentialed access for that volume, but grants no protection beyond
that single username/password shared by anyone who has it.

:memo:
Enabling the SMB gateway grants this controller's `ClusterRole` `get`,
`list`, `watch`, `create`, `patch`, and `delete` on `Secret` objects
**cluster-wide, in every namespace** — not just the ones it creates itself.
This is a materially larger blast radius than the PV/PVC/Service permissions
it already had. Review this trade-off before deploying the updated RBAC in
a multi-tenant cluster.

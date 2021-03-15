# Deploying the Capsule-proxy

This project is an add-on for [Capsule](https://github.com/clastix/capsule), the operator providing multi-tenancy in Kubernetes.

## Requirements

* [Helm 3](https://github.com/helm/helm/releases) is required when installing the Capsule-proxy chart. Follow Helm’s official [steps](https://helm.sh/docs/intro/install/) for installing helm on your particular operating system.

* A Kubernetes cluster 1.16+ with [Capsule](https://github.com/clastix/capsule) installed and following [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled:

    * PodNodeSelector
    * LimitRanger
    * ResourceQuota
    * MutatingAdmissionWebhook
    * ValidatingAdmissionWebhook

* A [`kubeconfig`](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/) file accessing the Kubernetes cluster with cluster admin permissions.

## Quick Start

The Capsule-proxy Chart can be used to instantly deploy the Capsule-proxy on your Kubernetes cluster.

1. Add this repository:

        $ helm repo add clastix https://clastix.github.io/charts

2. Install the Chart:

        $ helm install capsule-proxy clastix/capsule-proxy -n capsule-system

3. Show the status:

        $ helm status capsule-proxy -n capsule-system

4. Upgrade the Chart

        $ helm upgrade capsule-proxy clastix/capsule-proxy -n capsule-system

5. Uninstall the Chart
   
        $ helm uninstall capsule-proxy -n capsule-system

## Customize the installation

There are two methods for specifying overrides of values during chart installation: `--values` and `--set`.

The `--values` option is the preferred method because it allows you to keep your overrides in a YAML file, rather than specifying them all on the command line. Create a copy of the YAML file `values.yaml` and add your overrides to it.

Specify your overrides file when you install the chart:

        $ helm install capsule-proxy clastix/capsule-proxy --values myvalues.yaml -n capsule-system  

The values in your overrides file `myvalues.yaml` will override their counterparts in the chart’s values.yaml file. Any values in `values.yaml` that weren’t overridden will keep their defaults.

If you only need to make minor customizations, you can specify them on the command line by using the `--set` option. For example:

        $ helm install capsule-proxy clastix/capsule-proxy --values myvalues.yaml -n capsule-system

Here the values you can override:

Parameter | Description | Default
--- | --- | ---
`image.repository` | Set the image repository of the capsule-proxy. | `quay.io/clastix/capsule-proxy`
`image.pullPolicy` | Set the image pull policy. | `IfNotPresent`
`image.tag` | Overrides the image tag whose default is the chart. `appVersion` | `null`
`options.listeningPort` | Set the listening port of the capsule-proxy.| `9001`
`options.logLevel` | Set the log verbosity of the capsule-proxy with a value from 1 to 10.| `5`
`options.k8sControlPlaneUrl` | Set the URL of kubernetes control plane. | `https://kubernetes.default.svc`
`options.capsuleUserGroup` | Override the Capsule user group | `capsule.clastix.io`
`options.oidcUsernameClaim` | Override the OIDC field name used to identify the user | `preferred_username`
`options.enableSSL` | Specify if capsule-proxy will use SSL | `false`
`options.SSLDirectory` | Set the directory, where SSL certificate and keyfile will be located | `/opt/capsule-proxy`
`options.SSLCertFileName` | Set the name of SSL certificate file | `tls.crt`
`options.SSLKeyFileName` | Set the name of SSL key file | `tls.key`
`options.generateCertificates` | Specify if capsule-proxy will generate self-signed SSL certificates | `false`
`imagePullSecrets` | Configuration for `imagePullSecrets` so that you can use a private images registry. | `[]`
`serviceAccount.create` | Specifies whether a service account should be created. | `true`
`serviceAccount.annotations` | Annotations to add to the service account. | `{}`
`serviceAccount.name` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template | `capsule-proxy`
`podAnnotations` | Annotations to add to the capsule-proxy pod. | `{}`
`podSecurityContext` | Security context for the capsule-proxy pod. | `{}`
`securityContext` | Security context for the capsule-proxy deployment. | `{}`
`service.type` | Specifies the service type should be created. | `ClusterIP`
`service.port` | Specifies the service port number. | `9001`
`service.portName` | Specifies the service port name. | `proxy`
`service.nodePort` | Specifies the node port number (only for `NodePort` service type). | ``
`service.annotations` | Annotations to add to the service. | `{}`
`service.labels` | Labels to add to the service. | `{}`
`ingress.enabled` | Specifies whether an ingress should be created. | `false`
`ingress.annotations` | Annotations to add to the capsule-proxy ingress. | `true`
`ingress.hosts.host` | Set the host configuration for the capsule-proxy ingress. | `kube.clastix.io`
`ingress.hosts.path` | Set the path configuration for the capsule-proxy ingress. | `["/"]`
`ingress.tls` | Set the tls configuration for the capsule-proxy ingress. | `[]`
`resources.requests/cpu` | Set the CPU requests assigned to the controller. | `200m`
`resources.requests/memory` | Set the memory requests assigned to the controller. | `128Mi`
`resources.limits/cpu` | Set the CPU limits assigned to the controller. | `200m`
`resources.limits/cpu` | Set the memory limits assigned to the controller. | `128Mi`
`autoscaling.enabled` | Specifies whether an hpa for capsule-proxy should be created. | `true`
`autoscaling.minReplicas` | Set the minReplicas for capsule-proxy hpa. | `1`
`autoscaling.maxReplicas` | Set the maxReplicas for capsule-proxy hpa. | `5`
`autoscaling.targetCPUUtilizationPercentage` | Set the targetCPUUtilizationPercentage for capsule-proxy hpa. | `80`
`nodeSelector` | Set the node selector for the capsule-proxy pod. | `{}`
`tolerations` | Set list of tolerations for the capsule-proxy pod. | `[]`
`affinity` | Set affinity rules for the capsule-proxy pod. | `{}`
`replicaCount` | Set the replica count for capsule-proxy pod. | `1`
`hostNetwork` | Use the host network namespace for capsule-proxy pod. | `false`
`hostPort` | Binding the capsule-proxy listening port to the host port. | `false`

## Created resources

This Helm Chart cretes the following Kubernetes resources in the release namespace:

* Capsule-proxy Namespace
* Capsule-proxy Deployment
* Capsule-proxy Service
* RBAC Cluster Roles

And optionally, depending on the values set:

* Capsule-proxy ServiceAccount
* Capsule-proxy Ingress
* Capsule-proxy SSL certificate and key secret

## Using TLS with capsule-proxy

If you plan to use you own certificates for `capsule-proxy`, you need to create a secret in a namespace, where `capsule-proxy` will be deployed named the same, as your `capsule-proxy` deployment.

For example, if your deployment name is `capsule-filter` and it is deployed to `capsule-system` and `options.SSLCertFileName` is set to `tls.crt` and `options.SSLKeyFileName` is set to `tls.key` you secret should be like:

```yml
apiVersion: v1
data:
  tls.crt: <>
  tls.key: <>
kind: Secret
metadata:
  name: capsule-filter
  namespace: capsule-system
type: Opaque
```
Otherwise, you can set `options.generateCertificates` to `true` and self-signed certificates will be generated during deployment process by a post-install job.

# Deploying the Capsule-proxy

This project is an add-on for [Capsule](https://github.com/projectcapsule/capsule), the operator providing multi-tenancy in Kubernetes.

## Requirements

* [Helm 3](https://github.com/helm/helm/releases) is required when installing the Capsule-proxy chart. Follow Helm’s official [steps](https://helm.sh/docs/intro/install/) for installing helm on your particular operating system.

* A Kubernetes cluster 1.16+ with [Capsule](https://github.com/projectcapsule/capsule) installed and following [Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) enabled:

    * PodNodeSelector
    * LimitRanger
    * ResourceQuota
    * MutatingAdmissionWebhook
    * ValidatingAdmissionWebhook

* A [`kubeconfig`](https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/) file accessing the Kubernetes cluster with cluster admin permissions.

## Quick Start

The Capsule-proxy Chart can be used to instantly deploy the Capsule-proxy on your Kubernetes cluster.

1. Add this repository:

        $ helm repo add projectcapsule https://projectcapsule.github.io/charts

2. Install the Chart:

        $ helm install capsule-proxy projectcapsule/capsule-proxy -n capsule-system

3. Show the status:

        $ helm status capsule-proxy -n capsule-system

4. Upgrade the Chart

        $ helm upgrade capsule-proxy projectcapsule/capsule-proxy -n capsule-system

5. Uninstall the Chart

        $ helm uninstall capsule-proxy -n capsule-system

## Upgrading the Chart

Intsructions to upgrade the chart the versions, which may remove features or introduce breaking changes.

### 0.7.x

Introduces a new methode to manage all capsule-proxy CRDs and their lifecycle. We are no longer relying on the [native CRD hook with the Helm Chart](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations). The hook only allows to manage CRDs on install and uninstall but we can't deliver updates to the CRDs.
When you newly install the chart we recommend to set  `crds.install` to `true`. This will manage the CRDs with the Helm Chart.

If you are upgrading to this release, you can choose to set `global.crds.install` to `true` (by default `false`). However you need to add metadata to the existing CRDs so they can be correctly managed with the new flow. Run the following commands:

```bash
kubectl label crd proxysettings.capsule.clastix.io  app.kubernetes.io/managed-by=Helm
kubectl annotate crd proxysettings.capsule.clastix.io meta.helm.sh/release-namespace=capsule-system # might be different
kubectl annotate crd proxysettings.capsule.clastix.io meta.helm.sh/release-name=capsule-proxy # might be different
```

With the new CRD management we can release update CRDs bundled with the chart. The Chart can be uninstalled and the CRDs are still kept.

## Customize the installation

> When using **OCI** we recommend our dedicated [OCI Repository]() for this chart

There are two methods for specifying overrides of values during chart installation: `--values` and `--set`.

The `--values` option is the preferred method because it allows you to keep your overrides in a YAML file, rather than specifying them all on the command line. Create a copy of the YAML file `values.yaml` and add your overrides to it.

Specify your overrides file when you install the chart:

        $ helm install capsule-proxy projectcapsule/capsule-proxy --values myvalues.yaml -n capsule-system

The values in your overrides file `myvalues.yaml` will override their counterparts in the chart’s values.yaml file. Any values in `values.yaml` that weren’t overridden will keep their defaults.

If you only need to make minor customizations, you can specify them on the command line by using the `--set` option. For example:

        $ helm install capsule-proxy projectcapsule/capsule-proxy --set "kind=DaemonSet" -n capsule-system

### CustomResourceDefinition Lifecycle

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.annnotations | object | `{}` | Extra Annotations for CRDs |
| crds.install | bool | `true` | Install the CustomResourceDefinitions (This also manages the lifecycle of the CRDs for update operations) |
| crds.keep | bool | `true` | Keep the CustomResourceDefinitions (when the chart is deleted) |
| crds.labels | object | `{}` | Extra Labels for CRDs |

### Global Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| global.jobs.certs.affinity | object | `{}` | Set affinity rules |
| global.jobs.certs.annotations | object | `{}` | Annotations to add to the certgen job. |
| global.jobs.certs.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the post install certgen job |
| global.jobs.certs.image.registry | string | `"registry.k8s.io"` | Set the image repository of the post install certgen job |
| global.jobs.certs.image.repository | string | `"ingress-nginx/kube-webhook-certgen"` | Set the image repository of the post install certgen job |
| global.jobs.certs.image.tag | string | `"v1.6.5"` | Set the image tag of the post install certgen job |
| global.jobs.certs.nodeSelector | object | `{}` | Set the node selector |
| global.jobs.certs.podSecurityContext | object | `{"enabled":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the job pods. |
| global.jobs.certs.priorityClassName | string | `""` | Set a pod priorityClassName |
| global.jobs.certs.resources | object | `{}` | Job resources |
| global.jobs.certs.restartPolicy | string | `"Never"` | Set the restartPolicy |
| global.jobs.certs.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the job containers. |
| global.jobs.certs.tolerations | list | `[]` | Set list of tolerations |
| global.jobs.certs.topologySpreadConstraints | list | `[]` | Set Topology Spread Constraints |
| global.jobs.certs.ttlSecondsAfterFinished | int | `60` | Sets the ttl in seconds after a finished certgen job is deleted. Set to -1 to never delete. |
| global.jobs.kubectl.affinity | object | `{}` | Set affinity rules |
| global.jobs.kubectl.annotations | object | `{}` | Annotations to add to the job. |
| global.jobs.kubectl.image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy of the helm chart job |
| global.jobs.kubectl.image.registry | string | `"docker.io"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.repository | string | `"clastix/kubectl"` | Set the image repository of the helm chart job |
| global.jobs.kubectl.image.tag | string | `""` | Set the image tag of the helm chart job |
| global.jobs.kubectl.labels | object | `{}` | Labels to add to the job. |
| global.jobs.kubectl.nodeSelector | object | `{}` | Set the node selector |
| global.jobs.kubectl.podAnnotations | object | `{}` | Annotations to add to the job pod |
| global.jobs.kubectl.podLabels | object | `{}` | Labels to add to the job pod |
| global.jobs.kubectl.podSecurityContext | object | `{"enabled":false,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the job pods. |
| global.jobs.kubectl.priorityClassName | string | `""` | Set a pod priorityClassName |
| global.jobs.kubectl.resources | object | `{}` | Job resources |
| global.jobs.kubectl.restartPolicy | string | `"Never"` | Set the restartPolicy |
| global.jobs.kubectl.securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":false,"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the job containers. |
| global.jobs.kubectl.tolerations | list | `[]` | Set list of tolerations |
| global.jobs.kubectl.topologySpreadConstraints | list | `[]` | Set Topology Spread Constraints |
| global.jobs.kubectl.ttlSecondsAfterFinished | int | `60` | Sets the ttl in seconds after a finished certgen job is deleted. Set to -1 to never delete. |

### General Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Set affinity rules for the capsule-proxy pod. |
| crds.annnotations | object | `{}` | Extra Annotations for CRDs |
| crds.install | bool | `true` | Install the CustomResourceDefinitions (This also manages the lifecycle of the CRDs for update operations) |
| crds.keep | bool | `true` | Keep the CustomResourceDefinitions (when the chart is deleted) |
| crds.labels | object | `{}` | Extra Labels for CRDs |
| daemonset.hostNetwork | bool | `false` | Use the host network namespace for capsule-proxy pod. |
| daemonset.hostPort | bool | `false` | Binding the capsule-proxy listening port to the host port. |
| env | list | `[]` | Additional environment variables |
| hostNetwork | bool | `false` | When deployed as DaemonSet use |
| image.pullPolicy | string | `"IfNotPresent"` | Set the image pull policy. |
| image.registry | string | `"ghcr.io"` | Set the image registry for capsule-proxy |
| image.repository | string | `"projectcapsule/capsule-proxy"` | Set the image repository for capsule-proxy. |
| image.tag | string | `""` | Overrides the image tag whose default is the chart appVersion. |
| imagePullSecrets | list | `[]` | Configuration for `imagePullSecrets` so that you can use a private images registry. |
| jobs | object | `{"certs":{}}` | Deprecated: Use `global.jobs.certs` instead |
| kind | string | `"Deployment"` | Set the deployment mode of the capsule-proxy as `Deployment` or `DaemonSet`. |
| livenessProbe | object | `{"enabled":true,"httpGet":{"path":"/healthz/","port":"probe","scheme":"HTTP"},"initialDelaySeconds":20}` | Proxy Liveness-Probe |
| nodeSelector | object | `{}` | Set the node selector for the capsule-proxy pod. |
| podAnnotations | object | `{}` | Annotations to add to the capsule-proxy pod. |
| podLabels | object | `{}` | Labels to add to the capsule-proxy pod. |
| podSecurityContext | object | `{"enabled":true,"seccompProfile":{"type":"RuntimeDefault"}}` | Security context for the capsule-proxy pod. |
| priorityClassName | string | `""` | Specifies PriorityClass of the capsule-proxy pod. |
| rbac.clusterRole | string | `"cluster-admin"` | Controller ClusterRole |
| rbac.enabled | bool | `true` | Enable Creation of ClusterRoles |
| readinessProbe | object | `{"enabled":true,"httpGet":{"path":"/readyz/","port":"probe","scheme":"HTTP"},"initialDelaySeconds":5}` | Proxy Readyness-Probe |
| replicaCount | int | `1` | Set the replica count for capsule-proxy pod. |
| resources.limits.cpu | string | `"200m"` | Set the CPU requests assigned to the controller. |
| resources.limits.memory | string | `"128Mi"` | Set the memory requests assigned to the controller. |
| resources.requests.cpu | string | `"200m"` | Set the CPU limits assigned to the controller. |
| resources.requests.memory | string | `"128Mi"` | Set the memory limits assigned to the controller. |
| restartPolicy | string | `"Always"` | Set the restartPolicy for the capsule-proxy pod. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"enabled":true,"readOnlyRootFilesystem":true,"runAsGroup":1002,"runAsNonRoot":true,"runAsUser":1002}` | Security context for the capsule-proxy container. |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account. |
| serviceAccount.create | bool | `true` | Specifies whether a service account should be created. |
| serviceAccount.name | string | `capsule-proxy` | The name of the service account to use. If not set and `serviceAccount.create=true`, a name is generated using the fullname template |
| tolerations | list | `[]` | Set list of tolerations for the capsule-proxy pod. |
| topologySpreadConstraints | list | `[]` | Topology Spread Constraints for the capsule-proxy pod. |
| volumeMounts | list | `[]` | Additional volume mounts |
| volumes | list | `[]` | Additional volumes |

### Controller Options Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| options.SSLCertFileName | string | `"tls.crt"` | Set the name of SSL certificate file |
| options.SSLDirectory | string | `"/opt/capsule-proxy"` | Set the directory, where SSL certificate and keyfile will be located |
| options.SSLKeyFileName | string | `"tls.key"` | Set the name of SSL key file |
| options.additionalSANs | list | `[]` | Specify additional subject alternative names for the self-signed SSL |
| options.authPreferredTypes | string | `"BearerToken,TLSCertificate"` | Authentication types to be used for requests. Possible Auth Types: [BearerToken, TLSCertificate] |
| options.capsuleConfigurationName | string | `"default"` | Name of the CapsuleConfiguration custom resource used by Capsule, required to identify the user groups |
| options.certificateVolumeName | string | `""` | Specify an override for the Secret containing the certificate for SSL. Default value is empty and referring to the generated certificate. |
| options.clientConnectionBurst | int | `30` | Burst to use for interacting with kubernetes API Server. |
| options.clientConnectionQPS | int | `20` | QPS to use for interacting with Kubernetes API Server. |
| options.disableCaching | bool | `false` | Disable the go-client caching to hit directly the Kubernetes API Server, it disables any local caching as the rolebinding reflector. |
| options.enableSSL | bool | `true` | Specify if capsule-proxy will use SSL |
| options.extraArgs | list | `[]` | A list of extra arguments to add to the capsule-proxy. |
| options.generateCertificates | bool | `true` | Specify if capsule-proxy will generate self-signed SSL certificates |
| options.ignoredUserGroups | list | `[]` | Define which groups must be ignored while proxying requests |
| options.leaderElection | bool | `false` | Set leader election to true if you are running n-replicas |
| options.listeningPort | int | `9001` | Set the listening port of the capsule-proxy |
| options.logLevel | int | `4` | Set the log verbosity of the capsule-proxy with a value from 1 to 10 |
| options.oidcUsernameClaim | string | `"preferred_username"` | Specify if capsule-proxy will use SSL |
| options.pprof | bool | `false` | Enable Pprof for profiling |
| options.roleBindingReflector | bool | `false` | Enable the rolebinding reflector, which allows to list the namespaces, where a rolebinding mentions a user. |
| options.rolebindingsResyncPeriod | string | `"10h"` | Set the role bindings reflector resync period, a local cache to store mappings between users and their namespaces. [Use a lower value in case of flaky etcd server connections.](https://github.com/projectcapsule/capsule-proxy/issues/174) |

### Cert-Manager Parameters

You can manage the certificate with the help of [cert-manager](https://cert-manager.io/docs/). By default the chart will create a self-signed certificate.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| certManager.certificate.dnsNames | list | `[]` | Additional DNS Names to include in certificate |
| certManager.certificate.fields | object | `{"privateKey":{"rotationPolicy":"Always"}}` | Additional fields to include in certificate |
| certManager.certificate.includeInternalServiceNames | bool | `true` | Include internal service names in certificate (disable if you create a public cert) |
| certManager.certificate.ipAddresses | list | `[]` | Additional IP Addresses to include in certificate |
| certManager.certificate.uris | list | `[]` | Additional URIs to include in certificate |
| certManager.externalCA.enabled | bool | `false` | Set if want cert manager to sign certificates with an external CA |
| certManager.externalCA.secretName | string | `""` |  |
| certManager.generateCertificates | bool | `false` | Set if the cert manager will generate SSL certificates (self-signed or CA-signed) |
| certManager.issuer.kind | string | `"Issuer"` | Set if the cert manager will generate either self-signed or CA signed SSL certificates. Its value will be either Issuer or ClusterIssuer |
| certManager.issuer.name | string | `""` | Set the name of the ClusterIssuer if issuer kind is ClusterIssuer and if cert manager will generate CA signed SSL certificates |

### Webhook Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| webhooks.certificate.dnsNames | list | `[]` | Additional DNS Names to include in certificate |
| webhooks.certificate.fields | object | `{"privateKey":{"rotationPolicy":"Always"}}` | Additional fields to include in certificate |
| webhooks.certificate.ipAddresses | list | `[]` | Additional IP Addresses to include in certificate |
| webhooks.certificate.uris | list | `[]` | Additional URIs to include in certificate |
| webhooks.enabled | bool | `false` | Enable the usage of mutating and validating webhooks |
| webhooks.service.caBundle | string | `""` | CABundle for the webhook service |
| webhooks.service.name | string | `""` | Custom service name for the webhook service |
| webhooks.service.namespace | string | `""` | Custom service namespace for the webhook service |
| webhooks.service.port | string | `nil` | Custom service port for the webhook service |
| webhooks.service.url | string | `""` | The URL where the capsule webhook services are running (Overwrites cluster scoped service definition) |

### Service Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| service.annotations | object | `{}` | Annotations to add to the service. |
| service.labels | object | `{}` | Labels to add to the service. |
| service.nodePort | int | `0` | Specifies the node port number (only for `NodePort` service type). |
| service.port | int | `9001` | Specifies the service port number. |
| service.portName | string | `"proxy"` | Specifies the service port name. |
| service.type | string | `"ClusterIP"` | Specifies the service type should be created (`ClusterIP`, `NodePort`or `LoadBalancer`) |

### Ingress Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| ingress.annotations | object | `{}` | Annotations to add to the ingress. |
| ingress.className | string | `""` | Set the IngressClass to use for the capsule-proxy ingress (do not set via annotations if setting here). |
| ingress.enabled | bool | `false` | Specifies whether an ingress should be created. |
| ingress.hosts[0] | object | `{"host":"projectcapsule.dev","paths":["/"]}` | Set the host configuration for the capsule-proxy ingress. |
| ingress.hosts[0].paths | list | `["/"]` | Set the path configuration for the capsule-proxy ingress. |
| ingress.labels | object | `{}` | Labels to add to the ingress. |
| ingress.tls | list | `[]` | Set the tls configuration for the capsule-proxy ingress. |

### Autoscaler Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| autoscaling.annotations | object | `{}` | Annotations to add to the hpa. |
| autoscaling.behavior | object | `{}` | HPA [behavior](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) |
| autoscaling.enabled | bool | `false` | Specifies whether an hpa for capsule-proxy should be created. |
| autoscaling.labels | object | `{}` | Labels to add to the hpa. |
| autoscaling.maxReplicas | int | `3` | Set the maxReplicas for capsule-proxy hpa. |
| autoscaling.metrics | list | `[]` | Custom [metrics-objects](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/#autoscaling-on-multiple-metrics-and-custom-metrics) for capsule-proxy hpa |
| autoscaling.minReplicas | int | `1` | Set the minReplicas for capsule-proxy hpa. |
| autoscaling.targetCPUUtilizationPercentage | int | `0` | Set the targetCPUUtilizationPercentage for capsule-proxy hpa. |
| autoscaling.targetMemoryUtilizationPercentage | int | `0` | Set the targetMemoryUtilizationPercentage for capsule-proxy hpa. |

### ServiceMonitor Parameters

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| serviceMonitor.annotations | object | `{}` | Assign additional Annotations |
| serviceMonitor.enabled | bool | `false` | Enable ServiceMonitor |
| serviceMonitor.endpoint.interval | string | `"15s"` | Set the scrape interval for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.metricRelabelings | list | `[]` | Set metricRelabelings for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.relabelings | list | `[]` | Set relabelings for the endpoint of the serviceMonitor |
| serviceMonitor.endpoint.scrapeTimeout | string | `""` | Set the scrape timeout for the endpoint of the serviceMonitor |
| serviceMonitor.labels | object | `{}` | Assign additional labels according to Prometheus' serviceMonitorSelector matching labels |
| serviceMonitor.matchLabels | object | `{}` | Change matching labels |
| serviceMonitor.namespace | string | `""` | Install the ServiceMonitor into a different Namespace, as the monitoring stack one (default: the release one) |
| serviceMonitor.serviceAccount.name | string | `""` |  |
| serviceMonitor.serviceAccount.namespace | string | `""` |  |
| serviceMonitor.targetLabels | list | `[]` | Set targetLabels for the serviceMonitor |

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

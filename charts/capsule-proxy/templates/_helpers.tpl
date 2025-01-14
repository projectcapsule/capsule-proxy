{{/*
Expand the name of the chart.
*/}}
{{- define "capsule-proxy.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "capsule-proxy.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "capsule-proxy.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "capsule-proxy.labels" -}}
helm.sh/chart: {{ include "capsule-proxy.chart" . }}
{{ include "capsule-proxy.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "capsule-proxy.selectorLabels" -}}
app.kubernetes.io/name: {{ include "capsule-proxy.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "capsule-proxy.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "capsule-proxy.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the fully-qualified Docker image to use
*/}}
{{- define "capsule-proxy.fullyQualifiedDockerImage" -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository ( .Values.image.tag | default (printf "v%s" .Chart.AppVersion) ) -}}
{{- end }}

{{/*
Create CA secret name for the capsule proxy
*/}}
{{- define "capsule-proxy.caSecretName" -}}
{{- if .Values.certManager.externalCA.enabled -}}
{{- printf "%s" .Values.certManager.externalCA.secretName -}}
{{- else -}}
{{- printf "%s-root-secret" (include "capsule-proxy.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Create Cert Manager issuer name for the capsule proxy
*/}}
{{- define "capsule-proxy.certManager.issuerName" -}}
{{- if eq .Values.certManager.issuer.kind "ClusterIssuer" -}}
{{- printf "%s" .Values.certManager.issuer.name -}}
{{- else -}}
{{- printf "%s-ca-issuer" (include "capsule-proxy.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Render the CLI flag --host values for the self-signed certificate generator
*/}}
{{- define "capsule-proxy.certJob.SAN" -}}
{{- $name := ( include "capsule-proxy.fullname" . ) -}}
{{- $fullname := printf "%s.%s.svc" ( include "capsule-proxy.fullname" . ) ( .Release.Namespace ) -}}
{{- $values := append .Values.options.additionalSANs $name -}}
{{- $values = append $values $fullname -}}
{{ join "," $values }}
{{- end -}}



{{/*
Capsule Webhook service (Called with $.Path)

*/}}
{{- define "capsule-proxy.webhooks.service" -}}
  {{- include "capsule-proxy.webhooks.cabundle" $.ctx | nindent 0 }}
  {{- if $.ctx.Values.webhooks.service.url }}
url: {{ printf "%s/%s" (trimSuffix "/" $.ctx.Values.webhooks.service.url ) (trimPrefix "/" (required "Path is required for the function" $.path)) }}
  {{- else }}
service:
  name: {{ default (printf "%s-webhook-service" (include "capsule-proxy.fullname" $.ctx)) $.ctx.Values.webhooks.service.name }}
  namespace: {{ default $.ctx.Release.Namespace $.ctx.Values.webhooks.service.namespace }}
  port: {{ default 443 $.ctx.Values.webhooks.service.port }}
  path: {{ required "Path is required for the function" $.path }}
  {{- end }}
{{- end }}

{{/*
Capsule Webhook endpoint CA Bundle
*/}}
{{- define "capsule-proxy.webhooks.cabundle" -}}
  {{- if $.Values.webhooks.service.caBundle -}}
caBundle: {{ $.Values.webhooks.service.caBundle -}}
  {{- end -}}
{{- end -}}

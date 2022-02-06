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
{{- printf "%s:%s" .Values.image.repository ( .Values.image.tag | default (printf "v%s" .Chart.AppVersion) ) -}}
{{- end }}

{{/*
Create the certs jobs fully-qualified Docker image to use
*/}}
{{- define "capsule.jobs.certsFullyQualifiedDockerImage" -}}
{{- printf "%s:%s" .Values.jobs.certs.repository .Values.jobs.certs.tag -}}
{{- end -}}

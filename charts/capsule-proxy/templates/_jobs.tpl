{{/*
Determine the Kubernetes version to use for jobsFullyQualifiedDockerImage tag
*/}}
{{- define "capsule-proxy.jobsTagKubeVersion" -}}
{{- if contains "-eks-" .Capabilities.KubeVersion.GitVersion }}
{{- print "v" .Capabilities.KubeVersion.Major "." (.Capabilities.KubeVersion.Minor | replace "+" "") -}}
{{- else }}
{{- print "v" .Capabilities.KubeVersion.Major "." .Capabilities.KubeVersion.Minor -}}
{{- end }}
{{- end }}

{{/*
Create the jobs fully-qualified Docker image to use
*/}}
{{- define "capsule-proxy.kubectlFullyQualifiedDockerImage" -}}
{{- if .Values.global.jobs.kubectl.image.tag }}
{{- printf "%s/%s:%s" .Values.global.jobs.kubectl.image.registry .Values.global.jobs.kubectl.image.repository .Values.global.jobs.kubectl.image.tag -}}
{{- else }}
{{- printf "%s/%s:%s" .Values.global.jobs.kubectl.image.registry .Values.global.jobs.kubectl.image.repository (include "capsule-proxy.jobsTagKubeVersion" .) -}}
{{- end }}
{{- end }}

{{/*
Create the certs jobs fully-qualified Docker image to use
*/}}
{{- define "capsule.jobs.certsFullyQualifiedDockerImage" -}}
{{- printf "%s/%s:%s" (default $.Values.global.jobs.certs.image.registry $.Values.jobs.certs.registry) (default $.Values.global.jobs.certs.image.repository $.Values.jobs.certs.repository) (default $.Values.global.jobs.certs.image.tag $.Values.jobs.certs.tag)  -}}
{{- end -}}

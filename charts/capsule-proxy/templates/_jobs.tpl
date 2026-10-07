{{/*
Get the upstream Kubernetes patch version for official kubectl image tags.
*/}}
{{- define "capsule-proxy.jobsTagKubeVersion" -}}
{{- $version := regexFind "[0-9]+[.][0-9]+[.][0-9]+" .Capabilities.KubeVersion.GitVersion -}}
{{- if not $version -}}
{{- fail (printf "unable to determine kubectl image tag from Kubernetes version %q" .Capabilities.KubeVersion.GitVersion) -}}
{{- end }}
{{- printf "v%s" $version -}}
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

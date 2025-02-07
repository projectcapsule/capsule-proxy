{{- define "capsule-proxy.crds.name" -}}
{{- printf "%s-crds" (include "capsule-proxy.name" $) -}}
{{- end }}

{{- define "capsule-proxy.crds.annotations" -}}
"helm.sh/hook": "pre-install,pre-upgrade"
"helm.sh/hook-delete-policy": "before-hook-creation,hook-succeeded"
  {{- with $.Values.global.jobs.annotations }}
    {{- . | toYaml | nindent 0 }}
  {{- end }}
{{- end }}

{{- define "capsule-proxy.crds.component" -}}
crd-install-hook
{{- end }}

{{- define "capsule-proxy.crds.regexReplace" -}}
{{- printf "%s" ($ | base | trimSuffix ".yaml" | regexReplaceAll "[_.]" "-") -}}
{{- end }}

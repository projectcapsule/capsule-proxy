{{- define "capsule-proxy.pod" -}}
metadata:
  {{- with .Values.podAnnotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  labels:
    {{- include "capsule-proxy.selectorLabels" . | nindent 4 }}
    {{- with .Values.podLabels }}
      {{- toYaml . | nindent 4 }}
    {{- end }}
spec:
  {{- if eq .Values.kind "DaemonSet" }}
    {{- if .Values.daemonset.hostNetwork }}
  hostNetwork: true
  dnsPolicy: ClusterFirstWithHostNet
    {{- end }}
  {{- end }}
  {{- with .Values.imagePullSecrets }}
  imagePullSecrets:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  serviceAccountName: {{ include "capsule-proxy.serviceAccountName" . }}
  securityContext:
    {{- toYaml .Values.podSecurityContext | nindent 4 }}
  priorityClassName: {{ .Values.priorityClassName }}
  {{- if .Values.options.enableSSL }}
  volumes:
  - name: certs
    secret:
      secretName: {{ .Values.options.certificateVolumeName | default  (include "capsule-proxy.fullname" .) }}
      defaultMode: 420
  {{- end }}
  {{- with .Values.topologySpreadConstraints }}
  topologySpreadConstraints: {{- toYaml . | nindent 4 }}
  {{- end }}
  containers:
  - name: {{ .Chart.Name }}
    securityContext:
      {{- toYaml .Values.securityContext | nindent 6 }}
    image: {{ include "capsule-proxy.fullyQualifiedDockerImage" . }}
    imagePullPolicy: {{ .Values.image.pullPolicy }}
    args:
    - --listening-port={{ .Values.options.listeningPort }}
    - --capsule-configuration-name={{ .Values.options.capsuleConfigurationName }}
    {{- range .Values.options.ignoredUserGroups }}
    - --ignored-user-group={{ . }}
    {{- end}}
    - --zap-log-level={{ .Values.options.logLevel }}
    - --enable-ssl={{ .Values.options.enableSSL }}
    - --oidc-username-claim={{ .Values.options.oidcUsernameClaim }}
    - --rolebindings-resync-period={{ .Values.options.rolebindingsResyncPeriod }}
    - --disable-caching={{ .Values.options.disableCaching }}
    - --auth-preferred-types={{ .Values.options.authPreferredTypes }}
    {{- if .Values.options.enableSSL }}
    - --ssl-cert-path={{ .Values.options.SSLDirectory }}/{{ .Values.options.SSLCertFileName }}
    - --ssl-key-path={{ .Values.options.SSLDirectory }}/{{ .Values.options.SSLKeyFileName }}
    {{- end }}
    - --client-connection-qps={{ .Values.options.clientConnectionQPS }}
    - --client-connection-burst={{ .Values.options.clientConnectionBurst }}
    ports:
    - name: proxy
      protocol: TCP
      containerPort: {{ .Values.options.listeningPort }}
      {{- if eq .Values.kind "DaemonSet" }}
        {{- if .Values.daemonset.hostPort }}
      hostPort: {{ .Values.options.listeningPort }}
        {{- end }}
      {{- end }}
    - name: metrics
      containerPort: 8080
      protocol: TCP
    - name: probe
      containerPort: 8081
      protocol: TCP
    readinessProbe:
      httpGet:
        path: /readyz/
        port: probe
        scheme: HTTP
    livenessProbe:
      httpGet:
        path: /healthz/
        port: probe
        scheme: HTTP
    resources:
      {{- toYaml .Values.resources | nindent 12 }}
    {{- if .Values.options.enableSSL }}
    volumeMounts:
    - mountPath: {{ .Values.options.SSLDirectory }}
      name: certs
    {{- end }}
  {{- with .Values.nodeSelector }}
  nodeSelector:
    {{- toYaml . | nindent 8 }}
  {{- end }}
  {{- with .Values.affinity }}
  affinity:
    {{- toYaml . | nindent 8 }}
  {{- end }}
  {{- with .Values.tolerations }}
  tolerations:
    {{- toYaml . | nindent 8 }}
  {{- end }}
  restartPolicy: {{ .Values.restartPolicy }}
{{- end -}}

{{/*
Common labels for a component. Usage:
  {{ include "pantahub.labels" (dict "ctx" . "name" "kafka") }}
*/}}
{{- define "pantahub.labels" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/part-of: pantahub
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
app.kubernetes.io/managed-by: {{ .ctx.Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .ctx.Chart.Name .ctx.Chart.Version }}
{{- end }}

{{/*
Selector labels (stable subset).
*/}}
{{- define "pantahub.selectorLabels" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .ctx.Release.Name }}
{{- end }}

{{/*
Public hostnames: <sub>.<domain> when ingress.domain is set, else the
per-host fallbacks (apiHost/hubHost/pvrHost).
*/}}
{{- define "pantahub.apiHost" -}}
{{- if .Values.ingress.domain }}api.{{ .Values.ingress.domain }}{{- else }}{{ .Values.ingress.apiHost }}{{- end }}
{{- end }}

{{- define "pantahub.hubHost" -}}
{{- if .Values.ingress.domain }}hub.{{ .Values.ingress.domain }}{{- else }}{{ .Values.ingress.hubHost }}{{- end }}
{{- end }}

{{- define "pantahub.pvrHost" -}}
{{- if .Values.ingress.domain }}pvr.{{ .Values.ingress.domain }}{{- else }}{{ .Values.ingress.pvrHost }}{{- end }}
{{- end }}

{{/* http or https depending on ingress TLS */}}
{{- define "pantahub.publicScheme" -}}
{{- if .Values.ingress.tls.enabled }}https{{- else }}http{{- end }}
{{- end }}

{{/*
Render a map of env vars as a container env list.
Usage: {{ include "pantahub.envMap" .Values.www.env | nindent 12 }}
*/}}
{{- define "pantahub.envMap" -}}
{{- range $k, $v := . }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- end }}

{{/*
Effective shared env (pantahub-env ConfigMap data). When a public domain is
configured, the host/scheme keys are derived from it and win over .Values.env.
*/}}
{{- define "pantahub.effectiveEnv" -}}
{{- $env := deepCopy .Values.env }}
{{- if and .Values.ingress.enabled .Values.ingress.domain }}
{{- $scheme := include "pantahub.publicScheme" . }}
{{- $api := include "pantahub.apiHost" . }}
{{- $_ := set $env "PANTAHUB_HOST" $api }}
{{- $_ := set $env "PANTAHUB_PORT" "" }}
{{- $_ := set $env "PANTAHUB_SCHEME" $scheme }}
{{- $_ := set $env "PANTAHUB_HOST_WWW" (include "pantahub.hubHost" .) }}
{{- $_ := set $env "PH_AUTH" (printf "%s://%s/auth" $scheme $api) }}
{{- end }}
{{- range $k, $v := $env }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end }}

{{/*
www container env: REACT_APP_* URLs follow the public domain when set.
*/}}
{{- define "pantahub.wwwEnv" -}}
{{- $env := deepCopy .Values.www.env }}
{{- if and .Values.ingress.enabled .Values.ingress.domain }}
{{- $scheme := include "pantahub.publicScheme" . }}
{{- $_ := set $env "REACT_APP_API_URL" (printf "%s://%s" $scheme (include "pantahub.apiHost" .)) }}
{{- $_ := set $env "REACT_APP_WWW_URL" (printf "%s://%s" $scheme (include "pantahub.hubHost" .)) }}
{{- $_ := set $env "REACT_APP_PVR_URL" (printf "%s://%s" $scheme (include "pantahub.pvrHost" .)) }}
{{- end }}
{{- include "pantahub.envMap" $env }}
{{- end }}

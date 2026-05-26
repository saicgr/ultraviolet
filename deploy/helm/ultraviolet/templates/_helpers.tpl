{{/*
Standard helm helpers for the Ultraviolet chart.
*/}}

{{- define "ultraviolet.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ultraviolet.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "ultraviolet.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ultraviolet.labels" -}}
helm.sh/chart: {{ include "ultraviolet.chart" . }}
{{ include "ultraviolet.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "ultraviolet.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ultraviolet.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "ultraviolet.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "ultraviolet.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
componentLabels accepts a component name (proxy|api|sync) and returns the
full label block with app.kubernetes.io/component set.
*/}}
{{- define "ultraviolet.componentLabels" -}}
{{ include "ultraviolet.labels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "ultraviolet.componentSelector" -}}
{{ include "ultraviolet.selectorLabels" . }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

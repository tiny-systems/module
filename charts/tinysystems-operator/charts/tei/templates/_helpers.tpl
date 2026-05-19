{{/*
Expand the name of the chart.
*/}}
{{- define "tei.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fullname suffixes the release name with the chart name, mirroring the
standard bitnami / common-chart pattern. Lets the operator chart
release scope every TEI resource so multiple modules with TEI bundles
in different namespaces don't collide.
*/}}
{{- define "tei.fullname" -}}
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

{{- define "tei.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "tei.labels" -}}
helm.sh/chart: {{ include "tei.chart" . }}
{{ include "tei.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "tei.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tei.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

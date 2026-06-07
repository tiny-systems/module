{{/*
Expand the name of the chart.
*/}}
{{- define "tinysystems-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "tinysystems-operator.fullname" -}}
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
Cluster-scoped resource name.

The platform reuses one helm release name (`<module-workspace-slug>/<module>`)
across every tenant that installs the same module — the release name is
derived from the MODULE's owning workspace, not the installing workspace.
That makes per-release `fullname` identical across tenants, so any
cluster-scoped resource named purely from fullname collides on the
SECOND tenant with `meta.helm.sh/release-namespace` ownership errors.

Cluster-scoped resources (ClusterRole, ClusterRoleBinding, webhook
configs, PriorityClass, …) MUST go through this helper so the name
embeds the install namespace. Namespaced resources stay on fullname.
*/}}
{{- define "tinysystems-operator.clusterScopedName" -}}
{{- printf "%s-%s" (include "tinysystems-operator.fullname" .) .Release.Namespace | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "tinysystems-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "tinysystems-operator.labels" -}}
helm.sh/chart: {{ include "tinysystems-operator.chart" . }}
{{ include "tinysystems-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "tinysystems-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tinysystems-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "tinysystems-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "tinysystems-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

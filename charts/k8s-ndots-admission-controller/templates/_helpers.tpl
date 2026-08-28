{{/*
Expand the name of the chart.
*/}}
{{- define "k8s-ndots-admission-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "k8s-ndots-admission-controller.fullname" -}}
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
{{- define "k8s-ndots-admission-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "k8s-ndots-admission-controller.labels" -}}
helm.sh/chart: {{ include "k8s-ndots-admission-controller.chart" . }}
{{ include "k8s-ndots-admission-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "k8s-ndots-admission-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-ndots-admission-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Common annotations for all resources
*/}}
{{- define "k8s-ndots-admission-controller.annotations" -}}
{{- with .Values.commonAnnotations }}
{{- toYaml . }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "k8s-ndots-admission-controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "k8s-ndots-admission-controller.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the TLS secret
*/}}
{{- define "k8s-ndots-admission-controller.tlsSecretName" -}}
{{- printf "%s-tls" (include "k8s-ndots-admission-controller.fullname" .) }}
{{- end }}

{{/*
Create the webhook service DNS name
*/}}
{{- define "k8s-ndots-admission-controller.serviceDnsName" -}}
{{- printf "%s.%s.svc" (include "k8s-ndots-admission-controller.fullname" .) .Release.Namespace }}
{{- end }}

{{/*
Create the issuer name
*/}}
{{- define "k8s-ndots-admission-controller.issuerName" -}}
{{- if .Values.tls.certManager.issuer.name }}
{{- .Values.tls.certManager.issuer.name }}
{{- else }}
{{- printf "%s-issuer" (include "k8s-ndots-admission-controller.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Render dns.options as a DNS_OPTIONS CSV: "name=value,flag,name2=value2".
Options without a value render as just the name (flags like edns0).
*/}}
{{- define "k8s-ndots-admission-controller.dnsOptionsCSV" -}}
{{- $pairs := list -}}
{{- range .Values.dns.options -}}
{{- if .value -}}
{{- $pairs = append $pairs (printf "%s=%v" .name .value) -}}
{{- else -}}
{{- $pairs = append $pairs (printf "%s" .name) -}}
{{- end -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end }}


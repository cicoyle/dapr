{{/*
Expand the name of the chart.
*/}}
{{- define "dapr_scheduler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "dapr_scheduler.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "dapr_scheduler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create initial cluster peer list.
*/}}
{{- define "dapr_scheduler.initialcluster" -}}
{{- print "dapr-scheduler-server-0=dapr-scheduler-server-0.dapr-scheduler-server." .Release.Namespace ".svc" .Values.global.dnsSuffix ":" .Values.ports.etcdRPCPort ",dapr-scheduler-server-1=dapr-scheduler-server-1.dapr-scheduler-server." .Release.Namespace ".svc" .Values.global.dnsSuffix ":" .Values.ports.etcdRPCPort ",dapr-scheduler-server-2=dapr-scheduler-server-2.dapr-scheduler-server." .Release.Namespace ".svc" .Values.global.dnsSuffix ":" .Values.ports.etcdRPCPort -}}
{{- end -}}

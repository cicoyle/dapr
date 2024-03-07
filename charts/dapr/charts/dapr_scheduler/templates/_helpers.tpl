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
Create initial cluster peer list dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.initialcluster" -}}
{{- $initialCluster := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $svcName := printf "%s.%s" $instanceName $namespace -}}
{{- $peer := printf "%s=http://%s:%d" $instanceName $svcName (int $.Values.ports.etcdRPCPeerPort) -}}
{{- $initialCluster = printf "%s%s" $initialCluster $peer -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $initialCluster = printf "%s," $initialCluster -}}
{{- end -}}
{{- end -}}
{{- $initialCluster -}}
{{- end -}}

{{/*
Create initial advertise peer URLs dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.initialadvertisepeerurls" -}}
{{- $advertisePeerURLs := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $svcName := printf "%s.%s.svc" $instanceName $namespace -}}
{{- $peerURL := printf "http://%s:%d" $svcName (int $.Values.ports.etcdRPCPeerPort) -}}
{{- $advertisePeerURLs = printf "%s%s" $advertisePeerURLs $peerURL -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $advertisePeerURLs = printf "%s," $advertisePeerURLs -}}
{{- end -}}
{{- end -}}
{{- $advertisePeerURLs -}}
{{- end -}}

{{/*
Create initial advertise peer URLs dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.advertiseclienturls" -}}
{{- $advertisePeerURLs := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $svcName := printf "%s.%s.svc" $instanceName $namespace -}}
{{- $peerURL := printf "http://%s:%d" $svcName (int $.Values.ports.etcdRPCPeerPort) -}}
{{- $advertisePeerURLs = printf "%s%s" $advertisePeerURLs $peerURL -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $advertisePeerURLs = printf "%s," $advertisePeerURLs -}}
{{- end -}}
{{- end -}}
{{- $advertisePeerURLs -}}
{{- end -}}


{{/*
Create initial listen client URLs dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.listenclienturls" -}}
{{- $listenClientURLs := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $podIP := index .Pod.Status.PodIP -}}
{{- $listenClientURL := printf "http://%s:{{ .Values.ports.etcdRPCClientPort }}" $podIP -}}
{{- $listenClientURLs = printf "%s%s" $listenClientURLs $listenClientURL -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $listenClientURLs = printf "%s," $listenClientURLs -}}
{{- end -}}
{{- end -}}
{{- $listenClientURLs -}}
{{- end -}}

{{/*
Create initial listen peer URLs dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.listenpeersurls" -}}
{{- $listenPeerURLs := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $podIP := index .Pod.Status.PodIP -}}
{{- $listenPeerURL := printf "http://%s:{{ .Values.ports.etcdRPCPeerPort }}" $podIP -}}
{{- $listenPeerURLs = printf "%s%s" $listenPeerURLs $listenPeerURL -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $listenPeerURLs = printf "%s," $listenPeerURLs -}}
{{- end -}}
{{- end -}}
{{- $listenPeerURLs -}}
{{- end -}}


{{/*
Create initial cluster peer list dynamically based on replicaCount.
*/}}
{{- define "dapr_scheduler.initialclustertest" -}}
{{- $initialCluster := "" -}}
{{- $namespace := .Release.Namespace -}}
{{- $replicaCount := int .Values.replicaCount -}}
{{- range $i, $e := until $replicaCount -}}
{{- $instanceName := printf "dapr-scheduler-server-%d" $i -}}
{{- $svcName := printf "%s.%s" $instanceName $namespace -}}
{{- $peer := printf "%s=http://%s.dapr-scheduler-server.%s.svc.cluster.local:%d" $instanceName $svcName $namespace (int $.Values.ports.etcdRPCPeerPort) -}}
{{- $initialCluster = printf "%s%s" $initialCluster $peer -}}
{{- if ne (int $i) (sub $replicaCount 1) -}}
{{- $initialCluster = printf "%s," $initialCluster -}}
{{- end -}}
{{- end -}}
{{- $initialCluster -}}
{{- end -}}
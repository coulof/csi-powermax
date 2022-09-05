{{/*
Return the appropriate sidecar images based on k8s version
*/}}
{{- define "csi-powermax.attacherImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "k8s.gcr.io/sig-storage/csi-attacher:v3.4.0" -}}
  {{- end -}}
{{- end -}}

{{- define "csi-powermax.provisionerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "k8s.gcr.io/sig-storage/csi-provisioner:v3.1.0" -}}
  {{- end -}}
{{- end -}}

{{- define "csi-powermax.snapshotterImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "k8s.gcr.io/sig-storage/csi-snapshotter:v5.0.1" -}}
  {{- end -}}
{{- end -}}

{{- define "csi-powermax.resizerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "k8s.gcr.io/sig-storage/csi-resizer:v1.4.0" -}}
  {{- end -}}
{{- end -}}

{{- define "csi-powermax.registrarImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.5.1" -}}
  {{- end -}}
{{- end -}}

{{- define "csi-powermax.healthmonitorImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
      {{- print "gcr.io/k8s-staging-sig-storage/csi-external-health-monitor-controller:v0.5.0" -}}
  {{- end -}}
{{- end -}}

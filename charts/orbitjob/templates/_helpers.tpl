{{- define "orbitjob.securityContext" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
runAsNonRoot: true
runAsUser: 65534
capabilities:
  drop: ["ALL"]
{{- end -}}

{{- define "orbitjob.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65534
seccompProfile:
  type: RuntimeDefault
{{- end -}}

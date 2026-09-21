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

{{/*
Fully qualified image reference. An empty registry falls back to the bare name.
Tag precedence: component value > global.imageTag > chart appVersion with a v prefix,
so an upgrade only touches Chart.yaml.
Usage: {{ include "orbitjob.image" (dict "root" $ "image" $image) }}
*/}}
{{- define "orbitjob.image" -}}
{{- $registry := .root.Values.global.imageRegistry -}}
{{- $tag := .image.tag | default .root.Values.global.imageTag | default (printf "v%s" .root.Chart.AppVersion) -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry .image.repository $tag -}}
{{- else -}}
{{- printf "%s:%s" .image.repository $tag -}}
{{- end -}}
{{- end -}}

{{/*
Image pull policy. Precedence: component value > global.imagePullPolicy > IfNotPresent.
For locally built images in kind: --set global.imagePullPolicy=Never
*/}}
{{- define "orbitjob.imagePullPolicy" -}}
{{- .image.pullPolicy | default .root.Values.global.imagePullPolicy | default "IfNotPresent" -}}
{{- end -}}

{{/*
Namespace-to-tenant mappings for the operator.

Renders the value or fails the render, so a bad mapping is caught by helm
rather than by a pod that exits on startup. An empty value makes the operator
crash-loop with "OPERATOR_NAMESPACE_TENANTS is required"; a malformed one --
no "=", an empty side, a trailing comma -- makes it exit too. Both are cheaper
to report here, before anything is created.
*/}}
{{- define "orbitjob.operatorNamespaceTenants" -}}
{{- $raw := required "operator.namespaceTenants is required when operator.enabled is true. Set it to \"namespace=tenant[,namespace=tenant]\", or set operator.enabled=false to install without the operator." .Values.operator.namespaceTenants -}}
{{- $entry := "[^=,]+=[^=,]+" -}}
{{- $pattern := printf "^%s(,%s)*$" $entry $entry -}}
{{- if not (regexMatch $pattern $raw) -}}
{{- fail (printf "operator.namespaceTenants must be \"namespace=tenant[,namespace=tenant]\", got %q" $raw) -}}
{{- end -}}
{{- $raw -}}
{{- end }}

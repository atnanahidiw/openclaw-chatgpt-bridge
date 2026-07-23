{{- define "openclaw-chatgpt-bridge.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "openclaw-chatgpt-bridge.fullname" -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}


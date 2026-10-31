{{/*
Expand the name of the chart.
*/}}
{{- define "product-discovery.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "product-discovery.fullname" -}}
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
Fully qualified name of the A2A agent Deployment/Service/HTTPRoute (<release>-a2a,
ADR 0007). The base is trimmed so the suffix never pushes the name past the
63-character DNS label limit.
*/}}
{{- define "product-discovery.a2aFullname" -}}
{{- printf "%s-a2a" (include "product-discovery.fullname" . | trunc 59 | trimSuffix "-") }}
{{- end }}

{{/*
Chart name and version as used by the chart label.
*/}}
{{- define "product-discovery.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "product-discovery.labels" -}}
helm.sh/chart: {{ include "product-discovery.chart" . }}
{{ include "product-discovery.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels. Every Deployment and Service also pins
app.kubernetes.io/component (api): a Service selecting on these two alone would
select every component a future change adds.
*/}}
{{- define "product-discovery.selectorLabels" -}}
app.kubernetes.io/name: {{ include "product-discovery.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "product-discovery.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "product-discovery.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding DATABASE_URL: the operator's own
(database.existingSecret) or the one this chart creates from database.url.
*/}}
{{- define "product-discovery.databaseSecretName" -}}
{{- if .Values.database.existingSecret }}
{{- .Values.database.existingSecret }}
{{- else }}
{{- include "product-discovery.fullname" . }}-database
{{- end }}
{{- end }}

{{/*
Image reference. The release job publishes the image as v<version> (and packages
the chart with appVersion <version>), so an empty image.tag means "v" + appVersion.
*/}}
{{- define "product-discovery.image" -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default (printf "v%s" .Chart.AppVersion) }}
{{- end }}

{{/*
MANDATORY: cmd/api exits at boot without DATABASE_URL (real Postgres only, no
in-memory mode). Fails the render naming the values instead of crash-looping.
*/}}
{{- define "product-discovery.requireDatabase" -}}
{{- if not (or .Values.database.url .Values.database.existingSecret) -}}
{{- fail "product-discovery requires database.url or database.existingSecret to be set (DATABASE_URL): the service runs on real Postgres only and exits at boot without it." -}}
{{- end -}}
{{- end -}}

{{/*
MANDATORY: cmd/api exits at boot without KAFKA_BROKERS (the three consumers,
the dead letters and /readyz). No default broker and no way to switch Kafka off.
*/}}
{{- define "product-discovery.requireKafka" -}}
{{- $_ := required "product-discovery requires kafka.brokers to be set (KAFKA_BROKERS, comma-separated): the service runs on real Kafka only and exits at boot without it." .Values.kafka.brokers -}}
{{- end -}}

{{/*
The three consumers always run under STABLE group ids that must come from
values (cmd/api exits at boot without any of them). A stable id is what lets a
rescheduled pod resume from its committed offsets; it is never derived from
the pod name.
*/}}
{{- define "product-discovery.requireConsumerGroups" -}}
{{- range $c := list (dict "key" "offerAvailabilityConsumerGroup" "env" "OFFER_AVAILABILITY_CONSUMER_GROUP") (dict "key" "productContentConsumerGroup" "env" "PRODUCT_CONTENT_CONSUMER_GROUP") (dict "key" "pricingConsumerGroup" "env" "PRICING_CONSUMER_GROUP") -}}
{{- if not (index $.Values.config $c.key) -}}
{{- fail (printf "config.%s is required (%s): the consumer always runs on real Kafka and cmd/api exits at boot without a group. Use a STABLE, service-prefixed id, never the pod name." $c.key $c.env) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
extraEnv may not bring back what the chart owns: no *_MODE variable (this
service has no mode: Postgres and Kafka are the only runtime), no
*_CONSUMER_GROUP (the group ids are config.* values), and none of the
variables the chart renders itself (the same variable is never set twice).
*/}}
{{- define "product-discovery.requireCleanExtraEnv" -}}
{{- $owned := list "HTTP_ADDR" "LOG_LEVEL" "DATABASE_URL" "MIGRATIONS_DATABASE_URL" "KAFKA_BROKERS" "PRICE_BAND_EDGES" "SHUTDOWN_DRAIN_DELAY" "OFFER_AVAILABILITY_CONSUMER_GROUP" "PRODUCT_CONTENT_CONSUMER_GROUP" "PRICING_CONSUMER_GROUP" "OFFER_AVAILABILITY_URL" "PRICING_URL" "PRODUCT_CONTENT_URL" -}}
{{- range .Values.extraEnv -}}
{{- $name := toString .name -}}
{{- if hasSuffix "_MODE" $name -}}
{{- fail (printf "extraEnv sets %s: *_MODE variables do not exist here - product-discovery runs on real Postgres and real Kafka only, with no permissive, off or in-memory mode." $name) -}}
{{- end -}}
{{- if hasSuffix "_CONSUMER_GROUP" $name -}}
{{- fail (printf "extraEnv sets %s: the consumer group ids are the stable values config.offerAvailabilityConsumerGroup, config.productContentConsumerGroup and config.pricingConsumerGroup." $name) -}}
{{- end -}}
{{- if has $name $owned -}}
{{- fail (printf "extraEnv sets %s, which the chart renders from its own values: use that value instead." $name) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
SHUTDOWN_DRAIN_DELAY in whole seconds (the binary accepts Go durations; the
chart accepts "Ns", "Nm" and "0" because it needs the number to size the grace
period). Unset = the binary's default, 5s.
*/}}
{{- define "product-discovery.drainSeconds" -}}
{{- $raw := toString .Values.config.shutdownDrainDelay -}}
{{- if eq $raw "" -}}5
{{- else if eq $raw "0" -}}0
{{- else if regexMatch "^[0-9]+s$" $raw -}}{{ trimSuffix "s" $raw }}
{{- else if regexMatch "^[0-9]+m$" $raw -}}{{ mul 60 (int (trimSuffix "m" $raw)) }}
{{- else -}}
{{- fail (printf "config.shutdownDrainDelay is %q - want whole seconds (\"5s\"), minutes (\"1m\") or \"0\"." $raw) -}}
{{- end -}}
{{- end -}}

{{/*
The graceful shutdown is sequential (cmd/api/main.go, cmd/api/workers.go):
SHUTDOWN_DRAIN_DELAY, the HTTP drain (10s), then the three consumers awaited up
to 10s EACH (3x10s) and pool close headroom (10s). A grace period that does not
exceed drain + 50s can SIGKILL a consumer mid-drain, stranding an uncommitted
offset.
*/}}
{{- define "product-discovery.requireGracePeriod" -}}
{{- $need := add (int (include "product-discovery.drainSeconds" .)) 50 -}}
{{- if le (int .Values.terminationGracePeriodSeconds) $need -}}
{{- fail (printf "terminationGracePeriodSeconds is %v - it must exceed %v (SHUTDOWN_DRAIN_DELAY %vs + HTTP 10s + 3 consumers x 10s + pool close 10s): a shorter period SIGKILLs a consumer mid-drain." .Values.terminationGracePeriodSeconds $need (include "product-discovery.drainSeconds" .)) -}}
{{- end -}}
{{- end -}}

{{/*
The port of HTTP_ADDR is the container port the probes and the Service target.
*/}}
{{- define "product-discovery.requireHttpPort" -}}
{{- $port := trimPrefix ":" (regexFind ":[0-9]+$" (toString .Values.config.httpAddr)) -}}
{{- if ne $port (toString .Values.service.targetPort) -}}
{{- fail (printf "config.httpAddr is %q but service.targetPort is %v: the container port, the probes and the Service must agree." .Values.config.httpAddr .Values.service.targetPort) -}}
{{- end -}}
{{- end -}}

{{/*
cmd/rebuild (ADR 0003 S5, ADR 0005) is a one-shot ops job, rendered only when
rebuild.enabled. Its three producer base URLs are mandatory then; the error
names the missing value (cmd/rebuild exits without it).
*/}}
{{- define "product-discovery.requireRebuild" -}}
{{- if .Values.rebuild.enabled -}}
{{- range $s := list (dict "key" "offerAvailabilityUrl" "env" "OFFER_AVAILABILITY_URL") (dict "key" "pricingUrl" "env" "PRICING_URL") (dict "key" "productContentUrl" "env" "PRODUCT_CONTENT_URL") -}}
{{- if not (index $.Values.rebuild.sources $s.key) -}}
{{- fail (printf "rebuild.sources.%s is required when rebuild.enabled is true (%s): cmd/rebuild exits at boot without it." $s.key $s.env) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
The A2A agent (cmd/a2a, ADR 0007) listens on a2a.httpAddr; the container port,
the probes and the Service must agree, or the pod is never ready. The agent
needs DATABASE_URL like the api (checked by requireDatabase, which the api
Deployment always includes).
*/}}
{{- define "product-discovery.requireA2a" -}}
{{- if .Values.a2a.enabled -}}
{{- $port := trimPrefix ":" (regexFind ":[0-9]+$" (toString .Values.a2a.httpAddr)) -}}
{{- if ne $port (toString .Values.a2a.service.targetPort) -}}
{{- fail (printf "a2a.httpAddr is %q but a2a.service.targetPort is %v: the container port, the probes and the Service must agree." .Values.a2a.httpAddr .Values.a2a.service.targetPort) -}}
{{- end -}}
{{- if and .Values.a2a.route.enabled (not .Values.a2a.route.path) -}}
{{- fail "a2a.route.path is required when a2a.route.enabled is true: the gateway needs the prefix to route and rewrite to /." -}}
{{- end -}}
{{- if and .Values.a2a.agentUrl (not (regexMatch "^https?://[^/]+" (toString .Values.a2a.agentUrl))) -}}
{{- fail (printf "a2a.agentUrl is %q: it must be an absolute http(s) URL (cmd/a2a refuses to boot otherwise)." .Values.a2a.agentUrl) -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Every render-time requirement of the real runtime, in one include. The api
Deployment (always rendered) includes it, so a mis-set release fails
`helm template` / `helm install`.
*/}}
{{- define "product-discovery.requireRealRuntime" -}}
{{- include "product-discovery.requireDatabase" . -}}
{{- include "product-discovery.requireKafka" . -}}
{{- include "product-discovery.requireConsumerGroups" . -}}
{{- include "product-discovery.requireCleanExtraEnv" . -}}
{{- include "product-discovery.requireGracePeriod" . -}}
{{- include "product-discovery.requireHttpPort" . -}}
{{- include "product-discovery.requireRebuild" . -}}
{{- include "product-discovery.requireA2a" . -}}
{{- end -}}

#!/usr/bin/env python3
"""Assert this chart's Service selectors isolate each component.

Why this exists (fleet-wide): `app.kubernetes.io/name` + `instance` are identical
on every pod a release creates. A Service that selects on those two alone selects
ALL of them, which turns into a wrong answer the day a second component is added.
Every Deployment here carries `app.kubernetes.io/component` in selector.matchLabels
and its pod labels, and every Service selects on it, so each Service selects
EXACTLY ONE Deployment. This test fails if that ever stops being true.

product-discovery has exactly one always-on component: the api (REST plus the
three Kafka consumers - offer-availability, product-content, pricing - in one
process; the service publishes nothing, so there is no outbox relay). The
second binary, cmd/rebuild, is a one-shot ops Job (ADR 0003 S5, ADR 0005),
default off. The third, cmd/a2a (ADR 0007), is the optional READ-ONLY A2A agent:
its own Deployment, Service and HTTPRoute (component=a2a), default off, with no
Kafka. There is no mcp and no frontend: the chart renders nothing for binaries
that do not exist. The checks run with every optional template (HPA, Ingress,
HTTPRoute, the rebuild Job, the A2A agent and its route) enabled together.

Run: python3 charts/product-discovery/tests/test_service_selectors.py
Needs: helm, PyYAML.
"""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

CHART_DIR = Path(__file__).resolve().parents[1]
RELEASE = "product-discovery"
A2A = f"{RELEASE}-a2a"

# Dummy values, no password: the chart refuses to render without a database
# source, the Kafka brokers and the three consumer groups. None is ever
# dialled.
GROUPS = {
    "offerAvailabilityConsumerGroup": "product-discovery-offer-availability",
    "productContentConsumerGroup": "product-discovery-product-content",
    "pricingConsumerGroup": "product-discovery-pricing",
}


def runtime(skip: str | None = None) -> list[str]:
    """The mandatory values, minus the one named by skip (database, kafka or a group key)."""
    args: list[str] = []
    if skip != "database":
        args += ["--set", "database.url=postgres://u@example.invalid:5432/db"]
    if skip != "kafka":
        args += ["--set", "kafka.brokers=kafka.example.invalid:9092"]
    for key, value in GROUPS.items():
        if skip != key:
            args += ["--set", f"config.{key}={value}"]
    return args


BASE = runtime()
ENABLE_EVERYTHING = BASE + [
    "--set", "autoscaling.api.enabled=true",
    "--set", "gatewayApi.enabled=true",
    "--set", "ingress.enabled=true",
    "--set", "config.priceBandEdges=2500\\,5000\\,10000\\,25000",
    "--set", "config.shutdownDrainDelay=5s",
    "--set", "database.migrationsUrl=postgres://u@example.invalid:5432/direct",
    "--set", "a2a.enabled=true",
    "--set", "a2a.route.enabled=true",
    "--set", "a2a.route.parentRefs[0].name=warehouse-gateway",
    "--set", "a2a.route.parentRefs[0].namespace=kong",
    "--set", "rebuild.enabled=true",
    "--set", "rebuild.sources.offerAvailabilityUrl=http://offer-availability.example.invalid",
    "--set", "rebuild.sources.pricingUrl=http://pricing.example.invalid",
    "--set", "rebuild.sources.productContentUrl=http://product-content.example.invalid",
]

# Every variable cmd/api reads, with a dedicated chart value.
API_ENV = (
    "HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL", "KAFKA_BROKERS",
    "OFFER_AVAILABILITY_CONSUMER_GROUP", "PRODUCT_CONTENT_CONSUMER_GROUP", "PRICING_CONSUMER_GROUP",
    "PRICE_BAND_EDGES", "SHUTDOWN_DRAIN_DELAY",
)

# Every variable cmd/a2a reads (the agent is read-only: no Kafka, no consumer group).
A2A_ENV = ("A2A_ADDR", "A2A_AGENT_URL", "LOG_LEVEL", "DATABASE_URL", "MIGRATIONS_DATABASE_URL", "PRICE_BAND_EDGES")
A2A_FORBIDDEN_ENV = (
    "KAFKA_BROKERS", "EVENT_PUBLISHER", "HTTP_ADDR",
    "OFFER_AVAILABILITY_CONSUMER_GROUP", "PRODUCT_CONTENT_CONSUMER_GROUP", "PRICING_CONSUMER_GROUP",
)

# Every variable cmd/rebuild reads (cmd/rebuild/main.go's requiredEnv plus
# LOG_LEVEL): rendered by the Job, never the Deployment's problem.
REBUILD_ENV = ("DATABASE_URL", "OFFER_AVAILABILITY_URL", "PRICING_URL", "PRODUCT_CONTENT_URL", "LOG_LEVEL")


def helm_template(extra_args: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["helm", "template", RELEASE, str(CHART_DIR), *extra_args],
        capture_output=True, text=True,
    )


def render(extra_args: list[str]) -> list[dict]:
    result = helm_template(extra_args)
    if result.returncode != 0:
        raise SystemExit(f"FAIL: helm template {' '.join(extra_args)}:\n{result.stderr}")
    try:
        import yaml  # type: ignore
    except ModuleNotFoundError:  # pragma: no cover - environment guard
        print("SKIP: PyYAML not available; cannot assert selectors", file=sys.stderr)
        raise SystemExit(0)
    return [d for d in yaml.safe_load_all(result.stdout) if d]


def selector_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("selector") or {}


def pod_labels_of(doc: dict) -> dict:
    return doc.get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}


def matches(selector: dict, labels: dict) -> bool:
    return bool(selector) and all(labels.get(k) == v for k, v in selector.items())


def env_of(dep: dict) -> list[dict]:
    return dep["spec"]["template"]["spec"]["containers"][0].get("env", [])


def env_names(dep: dict) -> list[str]:
    return [e["name"] for e in env_of(dep)]


def env_value(dep: dict, name: str) -> str | None:
    return next((e.get("value") for e in env_of(dep) if e["name"] == name), None)


def check_a2a_route(docs: list[dict], failures: list[str]) -> None:
    """The a2a route exists, matches the context prefix and rewrites it to /."""
    routes = [d for d in docs if d.get("kind") == "HTTPRoute" and d["metadata"]["name"] == A2A]
    if len(routes) != 1:
        failures.append(f"the a2a HTTPRoute was not rendered exactly once ({len(routes)})")
        return
    rules = routes[0]["spec"]["rules"]
    if [m["path"]["value"] for r in rules for m in r["matches"]] != ["/api/a2a/product-discovery"]:
        failures.append("the a2a route must match /api/a2a/product-discovery")
    targets = [f["urlRewrite"]["path"].get("replacePrefixMatch") for r in rules for f in r.get("filters", []) if f["type"] == "URLRewrite"]
    if targets != ["/"]:
        failures.append(f"the a2a route must rewrite its prefix to /, got {targets}")


def check_a2a_deployment(dep: dict | None, failures: list[str]) -> None:
    """The agent is read-only: its env is the a2a contract, with no Kafka side."""
    if dep is None:
        failures.append("the a2a Deployment was not rendered with a2a.enabled=true")
        return
    container = dep["spec"]["template"]["spec"]["containers"][0]
    if container.get("command") != ["/app/a2a"]:
        failures.append("the a2a Deployment must run /app/a2a")
    names = env_names(dep)
    for want in A2A_ENV:
        if want not in names:
            failures.append(f"the a2a Deployment does not render {want}, which cmd/a2a reads")
    for bad in A2A_FORBIDDEN_ENV:
        if bad in names:
            failures.append(f"the read-only a2a Deployment must not render {bad}")
    if container.get("envFrom"):
        failures.append("the a2a Deployment must not mount the api ConfigMap (it would inherit HTTP_ADDR)")
    if env_value(dep, "A2A_ADDR") != ":8093":
        failures.append(f"the a2a agent must listen on :8093, got {env_value(dep, 'A2A_ADDR')!r}")
    url = env_value(dep, "A2A_AGENT_URL") or ""
    if not (url.startswith("http://") and url.endswith(f"-a2a.default.svc.cluster.local:8093/")):
        failures.append(f"A2A_AGENT_URL must default to the in-cluster Service DNS, got {url!r}")


def check_components(docs: list[dict], failures: list[str]) -> None:
    services = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Service"}
    deployments = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Deployment"}

    if set(deployments) != {RELEASE, A2A}:
        failures.append(f"the chart has exactly the api and a2a Deployments, rendered: {sorted(deployments)}")
    if set(services) != {RELEASE, A2A}:
        failures.append(f"the chart has exactly the api and a2a Services, rendered: {sorted(services)}")
    for name, component in ((RELEASE, "api"), (A2A, "a2a")):
        if name not in services:
            failures.append(f"the {component} Service was not rendered")
        elif selector_of(services[name]).get("app.kubernetes.io/component") != component:
            failures.append(f"the {component} Service selector must pin component={component}")

    # Every Deployment's own selector must pin a component too, and be
    # satisfied by its pod labels.
    for name, dep in deployments.items():
        match_labels = dep["spec"]["selector"].get("matchLabels") or {}
        if "app.kubernetes.io/component" not in match_labels:
            failures.append(f"Deployment {name} selector.matchLabels lacks app.kubernetes.io/component")
        if not matches(match_labels, pod_labels_of(dep)):
            failures.append(f"Deployment {name} pod labels do not satisfy its own selector")

    # The real invariant: each Service selects exactly one Deployment.
    for svc_name, svc in services.items():
        sel = selector_of(svc)
        hit = [d for d, dep in deployments.items() if matches(sel, pod_labels_of(dep))]
        if len(hit) != 1:
            failures.append(f"Service {svc_name} selects {len(hit)} Deployments {sorted(hit)}; expected exactly 1")

    # Ingress and the api HTTPRoute route to the api Service only; the a2a
    # HTTPRoute to the a2a Service only; the HPA owns the api replicas.
    for d in docs:
        if d.get("kind") == "Ingress":
            backends = {p["backend"]["service"]["name"] for r in d["spec"]["rules"] for p in r["http"]["paths"]}
            if backends != {RELEASE}:
                failures.append(f"the Ingress must route to the api Service only, got {sorted(backends)}")
        if d.get("kind") == "HTTPRoute":
            want = A2A if d["metadata"]["name"] == A2A else RELEASE
            backends = {b["name"] for r in d["spec"]["rules"] for b in r["backendRefs"]}
            if backends != {want}:
                failures.append(f"the HTTPRoute {d['metadata']['name']} must route to {want} only, got {sorted(backends)}")
    check_a2a_route(docs, failures)
    hpa = next((d for d in docs if d.get("kind") == "HorizontalPodAutoscaler"), None)
    if hpa is None or hpa["spec"]["scaleTargetRef"]["name"] != RELEASE:
        failures.append("the api HPA was not rendered (or does not scale the api Deployment)")
    elif "replicas" in deployments.get(RELEASE, {}).get("spec", {}):
        failures.append("the api Deployment must omit replicas when its HPA owns them")

    check_a2a_deployment(deployments.get(A2A), failures)

    api = deployments.get(RELEASE)
    if api is None:
        failures.append("the api Deployment was not rendered")
        return
    api_env = env_names(api)
    for want in API_ENV:
        if want not in api_env:
            failures.append(f"the api Deployment does not render {want} from its dedicated value")
    duplicates = sorted({n for n in api_env if api_env.count(n) > 1})
    if duplicates:
        failures.append(f"the api Deployment renders env vars twice: {duplicates}")
    for name, want in (("PRICE_BAND_EDGES", "2500,5000,10000,25000"), ("SHUTDOWN_DRAIN_DELAY", "5s")):
        if env_value(api, name) != want:
            failures.append(f"the api Deployment renders {name}={env_value(api, name)!r}, want {want!r}")

    # The rebuild Job: one-shot, never a second Service, and it carries exactly
    # the env cmd/rebuild reads (and no Kafka: the consumers resume from their
    # committed offsets afterwards).
    job = next((d for d in docs if d.get("kind") == "Job"), None)
    if job is None:
        failures.append("the rebuild Job was not rendered with rebuild.enabled=true")
    else:
        if job["spec"]["template"]["spec"].get("restartPolicy") != "Never":
            failures.append("the rebuild Job must run with restartPolicy: Never (a one-shot ops job)")
        if job["spec"]["template"]["spec"]["containers"][0].get("command") != ["/app/rebuild"]:
            failures.append("the rebuild Job must run /app/rebuild")
        job_env = env_names(job)
        for want in REBUILD_ENV:
            if want not in job_env:
                failures.append(f"the rebuild Job does not render {want}, which cmd/rebuild reads")
        if "KAFKA_BROKERS" in job_env:
            failures.append("the rebuild Job must not render KAFKA_BROKERS: cmd/rebuild needs no Kafka")
        if job["metadata"]["labels"].get("app.kubernetes.io/component") != "rebuild":
            failures.append("the rebuild Job must pin component=rebuild (never selectable by the api Service)")


def check_refusals(failures: list[str]) -> None:
    cases: list[tuple[str, list[str], str]] = [
        ("without a database source", runtime("database"), "requires database.url or database.existingSecret"),
        ("without kafka.brokers", runtime("kafka"), "kafka.brokers"),
        ("a *_MODE through extraEnv",
         BASE + ["--set", "extraEnv[0].name=CACHE_MODE", "--set", "extraEnv[0].value=permissive"], "extraEnv sets CACHE_MODE"),
        ("a consumer group through extraEnv",
         BASE + ["--set", "extraEnv[0].name=PRICING_CONSUMER_GROUP", "--set", "extraEnv[0].value=pod"],
         "extraEnv sets PRICING_CONSUMER_GROUP"),
        ("a chart-owned variable through extraEnv",
         BASE + ["--set", "extraEnv[0].name=KAFKA_BROKERS", "--set", "extraEnv[0].value=x:9092"], "extraEnv sets KAFKA_BROKERS"),
        ("a grace period under the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=50"], "terminationGracePeriodSeconds"),
        ("a grace period equal to the shutdown bounds", BASE + ["--set", "terminationGracePeriodSeconds=55"], "must exceed 55"),
        ("an http address on another port than the container's", BASE + ["--set", "config.httpAddr=:9090"], "config.httpAddr"),
        ("an a2a address off its container port", BASE + ["--set", "a2a.enabled=true", "--set", "a2a.httpAddr=:9090"], "a2a.httpAddr"),
        ("rebuild without a source URL",
         BASE + ["--set", "rebuild.enabled=true", "--set", "rebuild.sources.offerAvailabilityUrl=http://oa.invalid",
                 "--set", "rebuild.sources.pricingUrl=http://p.invalid"],
         "rebuild.sources.productContentUrl"),
    ]
    for key in GROUPS:
        cases.append((f"without config.{key}", runtime(key), f"config.{key} is required"))
    for label, args, needle in cases:
        result = helm_template(args)
        if result.returncode == 0 or needle not in result.stderr:
            failures.append(f"chart rendered (or failed for another reason) {label}")


def main() -> int:
    failures: list[str] = []

    check_components(render(ENABLE_EVERYTHING), failures)

    # Default values must not deploy an HPA, a route or the rebuild Job, nor
    # any optional env.
    defaults = render(BASE)
    stray = [d["kind"] for d in defaults if d.get("kind") in {"HorizontalPodAutoscaler", "Ingress", "HTTPRoute", "Job"}]
    if stray:
        failures.append(f"optional templates rendered with default values: {stray}")
    if any(d["metadata"]["name"] == A2A for d in defaults):
        failures.append("the a2a agent rendered with default values (it must be OFF by default)")
    dep = next(d for d in defaults if d.get("kind") == "Deployment")
    names = env_names(dep)
    off = {"PRICE_BAND_EDGES", "SHUTDOWN_DRAIN_DELAY"}
    if off & set(names):
        failures.append(f"these must be off with default values: {sorted(off & set(names))}")
    for want in ("KAFKA_BROKERS", *(k for k in API_ENV if k.endswith("_CONSUMER_GROUP"))):
        if want not in names:
            failures.append(f"{want} is mandatory and must always be rendered")

    check_refusals(failures)

    if failures:
        for f in failures:
            print(f"FAIL: {f}")
        return 1

    print("PASS: each Service selects exactly one Deployment (the api, the a2a agent); the HPA, Ingress, HTTPRoute and the rebuild Job "
          "are off by default and route to / run only what they own; every cmd/api env var has a dedicated value and "
          "the rebuild Job carries the env cmd/rebuild reads; the chart refuses to render without a database source, "
          "brokers or a consumer group, with a *_MODE or a consumer group in extraEnv, with a grace period that does "
          "not exceed the shutdown bounds, with an http address off the container port, with rebuild.enabled=true "
          "and a missing source URL, or with the a2a agent on a port other than its container's; the read-only a2a "
          "agent is off by default, selects only its own pods, renders no Kafka variable and its route rewrites to /")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

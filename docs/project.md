h2. Task: Implement and deploy a Kubernetes Mutating Admission Controller to set Pod dnsConfig ndots

h3. Scope

Build a MutatingAdmissionWebhook that sets the ndots DNS option in Pod.spec.dnsConfig.options to a configured value.
Support opt-in and opt-out via a Pod annotation (e.g., {{change-ndots: "true"/"false"}}).
Add a controller configuration to either honor the annotation or ignore it (always mutate).
Ensure pods created by all workload controllers (Deployments, StatefulSets, DaemonSets, Jobs/CronJobs) are covered.
Provide a Helm chart for deployment.
h3. Functional requirements

On Pod CREATE (and optionally UPDATE for template changes), mutate {{Pod.spec.dnsConfig.options}} to include or update the {{ndots}} option to the configured integer value.
If {{dnsConfig}} is absent, create {{dnsConfig}} with {{options}} containing {{ndots}}.
If {{options}} exists, update existing {{ndots}} entry or append if missing, ensuring only a single {{ndots}} option remains.
Annotation-based behavior: ** Define annotation key (default: {{change-ndots}}). ** Support modes: *** {{always}}: mutate all pods regardless of annotation. *** {{opt-in}}: mutate only when annotation == {{"true"}}. *** {{opt-out}}: mutate all pods unless annotation == {{"false"}}.
Do not mutate if Pod already has {{ndots}} equal to target value, to avoid no-op patches.
Allow to exclude {{kube-system}} and other critical namespaces via configuration.
Only mutate the Pod resource (not parent objects); ensure webhook rules cover Pods created via any workload controller in the cluster.
h3. Non-functional requirements

Compatible with Kubernetes >= 1.26.
Webhook served over TLS with proper CA bundle; use cert-manager or self-signed automation within the chart.
Resilient and idempotent patches; handle concurrent admissions.
Logging with mutation decisions (annotation mode, namespace, name, pre/post values).
Basic metrics (mutations count, skipped count, errors).
Minimal performance impact; timeouts and failurePolicy configurable (default: Ignore).
h3. Configuration

Values: ** {{ndots}}: integer (default 2; deploy QT with 2). ** {{annotationKey}}: string (default {{change-ndots}}). ** {{annotationMode}}: one of [{{always}}, {{opt-in}}, {{opt-out}}]. ** {{namespaceInclude}} / {{namespaceExclude}} lists (optional). ** {{webhook}}: *** rules: Pods, operations: CREATE (optional UPDATE). *** {{failurePolicy}}: Ignore or Fail. *** {{timeoutSeconds}}: default 10. ** TLS: *** {{useCertManager}}: true/false. *** cert secret name and namespace. ** Resources and {{nodeSelector}}/{{tolerations}} for the controller.
Helm chart exposes these values and sets validating configuration accordingly.
h3. Implementation outline

Language: Go preferred with controller-runtime or plain net/http; JSONPatch RFC 6902.
Admission logic: ** Decode Pod. ** Determine mutation necessity per {{annotationMode}} and current {{dnsConfig}}. ** Build patch to add/update {{Pod.spec.dnsConfig.options}} with {{{"name": "ndots", "value": "
Webhook configuration: ** MutatingWebhookConfiguration targeting: *** {{apiGroups}}: {{[""]}} (core) *** {{apiVersions}}: {{["v1"]}} *** {{resources}}: {{["pods"]}} *** {{operations}}: {{["CREATE"]}} (optional {{["UPDATE"]}}) *** {{sideEffects}}: {{None}} *** {{admissionReviewVersions}}: {{["v1"]}} ** Namespace selector support via configuration.
Certificates: ** If cert-manager enabled, include Issuer/Certificate templates and auto-inject CA bundle into MutatingWebhookConfiguration. ** If self-signed, use Helm hooks/job to generate and store certs, patch CA bundle.
RBAC: allow minimal required permissions (serve webhook; no cluster reads beyond webhook configuration if not needed).
Container image: build and publish to registry accessible by QT cluster.
h3. Testing and validation

Unit tests for: ** {{dnsConfig}} absent/present. ** Existing {{ndots}} updated vs appended. ** Annotation modes ({{always}}, {{opt-in}}, {{opt-out}}). ** No-op when {{ndots}} already matches target.
Integration tests in a dev cluster: ** Pods created via Deployment, StatefulSet, DaemonSet, Job, CronJob with and without annotation. ** Verify {{Pod.spec.dnsConfig.options}} contains {{ndots=2}}. ** Negative test: annotation {{false}} in opt-out mode prevents mutation. ** Namespaces excluded do not mutate.
Failure handling: webhook down, confirm {{failurePolicy}} behavior.
h3. Deliverables

Source code repository with: ** Controller implementation and Dockerfile. ** Helm chart (e.g., {{charts/k8s-dns-admission-controller}}) with {{values.yaml}} and templates for Deployment, Service, MutatingWebhookConfiguration, RBAC, certs. ** README with usage, configuration, and examples. ** CI workflow to build/push image and run tests.
Container image published to agreed registry.
h3. Acceptance criteria

Annotation opt-in/out works as specified, with configurable respect/ignore modes.
Webhook applies to pods from Deployments, StatefulSets, DaemonSets, Jobs, CronJobs.
Logs and metrics available; no significant admission latency introduced.
Documentation and tests included; CI builds pass.
h3. References

Kubernetes Pod DNSConfig: [https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.26/#poddnsconfig-v1-core]
package admission

import (
	"fmt"
	"log/slog"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
)

type Mutator struct {
	defaultSpec           DNSSpec
	specAnnotationKey     string
	strategyAnnotationKey string
	annotationChecker     *AnnotationChecker
	namespaceFilter       *NamespaceFilter
	logger                *slog.Logger
}

func NewMutator(cfg *config.Config, logger *slog.Logger) *Mutator {
	return &Mutator{
		defaultSpec:           DefaultDNSSpecFromConfig(cfg),
		specAnnotationKey:     cfg.SpecAnnotationKey,
		strategyAnnotationKey: cfg.StrategyAnnotationKey,
		annotationChecker:     NewAnnotationChecker(cfg.AnnotationKey, cfg.AnnotationMode),
		namespaceFilter:       NewNamespaceFilter(cfg.NamespaceInclude, cfg.NamespaceExclude, logger),
		logger:                logger,
	}
}

func (m *Mutator) Mutate(pod *corev1.Pod) ([]PatchOperation, error) {
	podName := getPodName(pod)
	if !m.namespaceFilter.ShouldMutate(pod.Namespace) {
		m.logger.Debug("skipping mutation due to namespace filter",
			"namespace", pod.Namespace,
			"name", podName,
		)
		return nil, nil
	}

	if !m.annotationChecker.ShouldMutate(pod.Annotations) {
		m.logger.Debug("skipping mutation due to annotation",
			"namespace", pod.Namespace,
			"name", podName,
		)
		return nil, nil
	}

	spec := m.effectiveSpec(pod, podName)
	return m.buildPatches(pod, spec), nil
}

// effectiveSpec resolves the DNS spec for a pod: the operator default, overlaid
// with any per-pod annotation spec and strategy. Annotation parse errors
// degrade to the default spec (fail-open) rather than propagating.
func (m *Mutator) effectiveSpec(pod *corev1.Pod, podName string) DNSSpec {
	spec := m.defaultSpec

	if m.specAnnotationKey != "" {
		if raw, ok := pod.Annotations[m.specAnnotationKey]; ok && raw != "" {
			overlay, err := ParseDNSSpecAnnotation(raw)
			if err != nil {
				m.logger.Warn("ignoring malformed DNS spec annotation, using default",
					"namespace", pod.Namespace,
					"name", podName,
					"error", err,
				)
			} else {
				spec = spec.MergeOverlay(overlay)
			}
		}
	}

	if m.strategyAnnotationKey != "" {
		if raw, ok := pod.Annotations[m.strategyAnnotationKey]; ok && raw != "" {
			strategy, err := ParseStrategy(raw)
			if err != nil {
				m.logger.Warn("ignoring invalid DNS strategy annotation, using default",
					"namespace", pod.Namespace,
					"name", podName,
					"error", err,
				)
			} else {
				spec.Strategy = strategy
			}
		}
	}

	return spec
}

// buildPatches produces the JSON Patch operations that reconcile the pod's DNS
// settings toward the desired spec. It never emits a patch that would produce
// an API-invalid pod, and creates parent paths before child operations.
func (m *Mutator) buildPatches(pod *corev1.Pod, spec DNSSpec) []PatchOperation {
	var patches []PatchOperation

	// Ensure /spec/dnsConfig exists before touching any subfield.
	if pod.Spec.DNSConfig == nil {
		obj := m.buildInitialDNSConfig(spec)
		if len(obj) > 0 {
			patches = append(patches, PatchOperation{
				Op:    "add",
				Path:  "/spec/dnsConfig",
				Value: obj,
			})
		}
	} else {
		patches = append(patches, m.optionPatches(pod.Spec.DNSConfig, spec)...)
		patches = append(patches, m.listPatches(pod.Spec.DNSConfig.Nameservers, "nameservers", spec.Nameservers, spec.Strategy)...)
		patches = append(patches, m.listPatches(pod.Spec.DNSConfig.Searches, "searches", spec.Searches, spec.Strategy)...)
	}

	patches = append(patches, m.policyPatches(pod, spec)...)

	return patches
}

// buildInitialDNSConfig assembles the dnsConfig object for a pod that has none.
// Unset/update strategies contribute nothing to a brand-new config.
func (m *Mutator) buildInitialDNSConfig(spec DNSSpec) map[string]interface{} {
	if spec.Strategy == StrategyUnset || spec.Strategy == StrategyUpdate {
		return nil
	}

	obj := map[string]interface{}{}
	if len(spec.Options) > 0 {
		obj["options"] = optionsToMaps(spec.Options)
	}
	if len(spec.Nameservers) > 0 {
		obj["nameservers"] = spec.Nameservers
	}
	if len(spec.Searches) > 0 {
		obj["searches"] = spec.Searches
	}
	return obj
}

// optionPatches reconciles dnsConfig.options against the spec.
func (m *Mutator) optionPatches(dnsConfig *corev1.PodDNSConfig, spec DNSSpec) []PatchOperation {
	if len(spec.Options) == 0 {
		return nil
	}

	if spec.Strategy == StrategyOverride {
		op := "add"
		if dnsConfig.Options != nil {
			op = "replace"
		}
		return []PatchOperation{{
			Op:    op,
			Path:  "/spec/dnsConfig/options",
			Value: optionsToMaps(spec.Options),
		}}
	}

	if dnsConfig.Options == nil {
		// merge/update: unset contributes nothing; update needs existing entries
		// (none exist), so only merge adds the array.
		if spec.Strategy != StrategyMerge {
			return nil
		}
		return []PatchOperation{{
			Op:    "add",
			Path:  "/spec/dnsConfig/options",
			Value: optionsToMaps(spec.Options),
		}}
	}

	if spec.Strategy == StrategyUnset {
		return m.optionRemovePatches(dnsConfig.Options, spec.Options)
	}

	var patches []PatchOperation
	for _, want := range spec.Options {
		idx := findOptionIndex(dnsConfig.Options, want.Name)
		switch {
		case idx == -1:
			// Absent: merge appends; update skips.
			if spec.Strategy == StrategyMerge {
				patches = append(patches, PatchOperation{
					Op:    "add",
					Path:  "/spec/dnsConfig/options/-",
					Value: optionToMap(want),
				})
			}
		case !optionValueEqual(dnsConfig.Options[idx], want):
			patches = append(patches, PatchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/dnsConfig/options/%d/value", idx),
				Value: derefValue(want.Value),
			})
		}
	}
	return patches
}

// optionRemovePatches emits remove ops for managed options present on the pod,
// in descending index order so earlier removals don't shift later indices.
func (m *Mutator) optionRemovePatches(existing, managed []corev1.PodDNSConfigOption) []PatchOperation {
	var indices []int
	for _, want := range managed {
		if idx := findOptionIndex(existing, want.Name); idx != -1 {
			indices = append(indices, idx)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))

	patches := make([]PatchOperation, 0, len(indices))
	for _, idx := range indices {
		patches = append(patches, PatchOperation{
			Op:   "remove",
			Path: fmt.Sprintf("/spec/dnsConfig/options/%d", idx),
		})
	}
	return patches
}

// listPatches reconciles a []string subfield (nameservers or searches).
func (m *Mutator) listPatches(existing []string, field string, want []string, strategy Strategy) []PatchOperation {
	if len(want) == 0 {
		return nil
	}
	path := "/spec/dnsConfig/" + field

	switch strategy {
	case StrategyUnset:
		if existing != nil {
			return []PatchOperation{{Op: "remove", Path: path}}
		}
		return nil
	case StrategyUpdate:
		if existing == nil {
			return nil
		}
		return []PatchOperation{{Op: "replace", Path: path, Value: want}}
	case StrategyOverride:
		op := "add"
		if existing != nil {
			op = "replace"
		}
		return []PatchOperation{{Op: op, Path: path, Value: want}}
	default: // merge
		if existing == nil {
			return []PatchOperation{{Op: "add", Path: path, Value: want}}
		}
		union := unionStrings(existing, want)
		if len(union) == len(existing) {
			return nil // nothing new
		}
		return []PatchOperation{{Op: "replace", Path: path, Value: union}}
	}
}

// policyPatches reconciles pod.Spec.DNSPolicy. It never sets DNSPolicy=None
// unless the resulting pod would have nameservers, since the API server rejects
// that combination.
func (m *Mutator) policyPatches(pod *corev1.Pod, spec DNSSpec) []PatchOperation {
	if spec.DNSPolicy == nil {
		return nil
	}
	want := *spec.DNSPolicy

	switch spec.Strategy {
	case StrategyUnset:
		m.logger.Warn("unset strategy is not meaningful for dnsPolicy; skipping")
		return nil
	case StrategyUpdate:
		if pod.Spec.DNSPolicy == "" {
			return nil
		}
	}

	if pod.Spec.DNSPolicy == want {
		return nil
	}

	if want == corev1.DNSNone && !willHaveNameservers(pod, spec) {
		m.logger.Warn("skipping dnsPolicy=None: would leave pod without nameservers",
			"name", getPodName(pod),
			"namespace", pod.Namespace,
		)
		return nil
	}

	op := "add"
	if pod.Spec.DNSPolicy != "" {
		op = "replace"
	}
	return []PatchOperation{{Op: op, Path: "/spec/dnsPolicy", Value: string(want)}}
}

// willHaveNameservers reports whether the mutated pod would end up with at least
// one nameserver, considering both the managed spec and existing pod config.
func willHaveNameservers(pod *corev1.Pod, spec DNSSpec) bool {
	if spec.Strategy != StrategyUnset && len(spec.Nameservers) > 0 {
		return true
	}
	if spec.Strategy == StrategyUnset {
		return false
	}
	return pod.Spec.DNSConfig != nil && len(pod.Spec.DNSConfig.Nameservers) > 0
}

func optionsToMaps(options []corev1.PodDNSConfigOption) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(options))
	for _, o := range options {
		out = append(out, optionToMap(o))
	}
	return out
}

func optionToMap(o corev1.PodDNSConfigOption) map[string]interface{} {
	m := map[string]interface{}{"name": o.Name}
	if o.Value != nil {
		m["value"] = *o.Value
	}
	return m
}

func optionValueEqual(a, b corev1.PodDNSConfigOption) bool {
	if a.Value == nil || b.Value == nil {
		return a.Value == nil && b.Value == nil
	}
	return *a.Value == *b.Value
}

func derefValue(v *string) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func unionStrings(existing, want []string) []string {
	seen := make(map[string]bool, len(existing))
	result := make([]string, len(existing))
	copy(result, existing)
	for _, s := range existing {
		seen[s] = true
	}
	for _, s := range want {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

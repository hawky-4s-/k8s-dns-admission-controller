package admission

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/hawky-4s-/k8s-ndots-admission-controller/internal/config"
)

// Strategy controls how managed DNS settings are applied to a pod.
type Strategy string

const (
	// StrategyMerge adds or updates managed keys, leaving others intact (default).
	StrategyMerge Strategy = "merge"
	// StrategyUpdate only changes a field/option if it is already present.
	StrategyUpdate Strategy = "update"
	// StrategyUnset removes the managed field/option.
	StrategyUnset Strategy = "unset"
	// StrategyOverride replaces the entire field.
	StrategyOverride Strategy = "override"
)

// ParseStrategy validates and normalizes a strategy string. An empty string
// defaults to StrategyMerge.
func ParseStrategy(s string) (Strategy, error) {
	switch Strategy(strings.ToLower(strings.TrimSpace(s))) {
	case "", StrategyMerge:
		return StrategyMerge, nil
	case StrategyUpdate:
		return StrategyUpdate, nil
	case StrategyUnset:
		return StrategyUnset, nil
	case StrategyOverride:
		return StrategyOverride, nil
	default:
		return "", fmt.Errorf("invalid strategy %q: must be merge, update, unset, or override", s)
	}
}

// DNSSpec is the desired DNS state the mutator applies to a pod. A nil pointer
// or empty slice means the corresponding field is unmanaged.
type DNSSpec struct {
	DNSPolicy   *corev1.DNSPolicy
	Nameservers []string
	Searches    []string
	Options     []corev1.PodDNSConfigOption
	Strategy    Strategy
}

// dnsSpecAnnotation is the on-the-wire shape of the injected DNS spec
// annotation. It embeds PodDNSConfig's JSON tags for lossless parsing.
type dnsSpecAnnotation struct {
	Nameservers []string                    `json:"nameservers,omitempty"`
	Searches    []string                    `json:"searches,omitempty"`
	Options     []corev1.PodDNSConfigOption `json:"options,omitempty"`
	DNSPolicy   *corev1.DNSPolicy           `json:"dnsPolicy,omitempty"`
	Strategy    string                      `json:"strategy,omitempty"`
}

// ParseDNSSpecAnnotation parses a JSON or YAML DNS spec (as carried in a pod
// annotation) into a DNSSpec. sigs.k8s.io/yaml handles both formats.
func ParseDNSSpecAnnotation(raw string) (DNSSpec, error) {
	var a dnsSpecAnnotation
	if err := yaml.Unmarshal([]byte(raw), &a); err != nil {
		return DNSSpec{}, fmt.Errorf("failed to parse DNS spec annotation: %w", err)
	}

	strategy, err := ParseStrategy(a.Strategy)
	if err != nil {
		return DNSSpec{}, err
	}

	return DNSSpec{
		DNSPolicy:   a.DNSPolicy,
		Nameservers: a.Nameservers,
		Searches:    a.Searches,
		Options:     a.Options,
		Strategy:    strategy,
	}, nil
}

// DefaultDNSSpecFromConfig builds the operator-configured default DNS spec.
// The ndots value is always seeded as an option named "ndots" unless the
// configured options already include one, preserving backward compatibility.
func DefaultDNSSpecFromConfig(cfg *config.Config) DNSSpec {
	strategy, err := ParseStrategy(cfg.DNSStrategy)
	if err != nil {
		// Config is validated at load time; fall back defensively.
		strategy = StrategyMerge
	}

	options := make([]corev1.PodDNSConfigOption, 0, len(cfg.DNSOptions)+1)
	hasNdots := false
	for _, o := range cfg.DNSOptions {
		opt := corev1.PodDNSConfigOption{Name: o.Name}
		if o.Value != "" {
			v := o.Value
			opt.Value = &v
		}
		if o.Name == "ndots" {
			hasNdots = true
		}
		options = append(options, opt)
	}
	if !hasNdots {
		v := fmt.Sprintf("%d", cfg.NdotsValue)
		options = append(options, corev1.PodDNSConfigOption{Name: "ndots", Value: &v})
	}

	spec := DNSSpec{
		Nameservers: cfg.DNSNameservers,
		Searches:    cfg.DNSSearches,
		Options:     options,
		Strategy:    strategy,
	}
	if cfg.DNSPolicy != "" {
		p := corev1.DNSPolicy(cfg.DNSPolicy)
		spec.DNSPolicy = &p
	}
	return spec
}

// MergeOverlay returns a copy of base with any field the overlay sets taking
// precedence. Options are merged by name: a same-named overlay option replaces
// the base option; other overlay options are appended.
func (base DNSSpec) MergeOverlay(overlay DNSSpec) DNSSpec {
	result := base

	if overlay.DNSPolicy != nil {
		result.DNSPolicy = overlay.DNSPolicy
	}
	if len(overlay.Nameservers) > 0 {
		result.Nameservers = overlay.Nameservers
	}
	if len(overlay.Searches) > 0 {
		result.Searches = overlay.Searches
	}
	if overlay.Strategy != "" {
		result.Strategy = overlay.Strategy
	}
	if len(overlay.Options) > 0 {
		result.Options = mergeOptions(base.Options, overlay.Options)
	}

	return result
}

// mergeOptions overlays options by name onto a copy of base.
func mergeOptions(base, overlay []corev1.PodDNSConfigOption) []corev1.PodDNSConfigOption {
	merged := make([]corev1.PodDNSConfigOption, len(base))
	copy(merged, base)

	for _, o := range overlay {
		if idx := findOptionIndex(merged, o.Name); idx != -1 {
			merged[idx] = o
		} else {
			merged = append(merged, o)
		}
	}
	return merged
}

// findOptionIndex returns the index of the option with the given name, or -1.
func findOptionIndex(options []corev1.PodDNSConfigOption, name string) int {
	for i, opt := range options {
		if opt.Name == name {
			return i
		}
	}
	return -1
}

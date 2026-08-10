package solve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/internal/catalog"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/codec"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

type bridge struct {
	component plugin.Component
	input     flow.Port
	output    flow.Port
}

type candidateIndex map[string][]bridge

func buildCandidateIndex(index catalog.Index, policy job.Policy, platform plan.Platform) candidateIndex {
	result := make(candidateIndex)
	for _, component := range index.Components() {
		shape := component.Ports()
		if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 || shape.Inputs[0].Multiplicity() != flow.One || shape.Outputs[0].Multiplicity() != flow.One {
			continue
		}
		if !compatibleContract(component.Contract(), policy, platform) {
			continue
		}
		key := shape.Inputs[0].Schema().Identity().String()
		result[key] = append(result[key], bridge{component: component, input: shape.Inputs[0], output: shape.Outputs[0]})
	}
	for key := range result {
		sort.Slice(result[key], func(left, right int) bool {
			return result[key][left].component.Identity().String() < result[key][right].component.Identity().String()
		})
	}
	return result
}

func compatibleContract(contract plugin.Contract, policy job.Policy, platform plan.Platform) bool {
	if !contract.Valid() {
		return false
	}
	if policy.Accuracy == job.ExactAccuracy && contract.Accuracy != plugin.ExactContract {
		return false
	}
	if policy.Repeatability == job.Repeatable && contract.Repeatability != plugin.RepeatableContract {
		return false
	}
	requiredArtifact := plugin.NoArtifactSupport
	switch policy.Artifact {
	case job.ArtifactStable:
		requiredArtifact = plugin.StableArtifactSupport
	case job.ArtifactPortable:
		requiredArtifact = plugin.PortableArtifactSupport
	}
	if contract.Artifact < requiredArtifact {
		return false
	}
	implementation := contract.Implementation
	allowed := policy.Implementation
	if implementation.Has(plugin.PureGoImplementation) && !allowed.PureGo ||
		implementation.Has(plugin.UnsafeImplementation) && !allowed.Unsafe ||
		implementation.Has(plugin.SIMDImplementation) && !allowed.SIMD ||
		implementation.Has(plugin.FMAImplementation) && !allowed.FMA ||
		implementation.Has(plugin.ParallelImplementation) && !allowed.Parallel ||
		implementation.Has(plugin.NativeImplementation) && !allowed.Native ||
		implementation.Has(plugin.DeviceImplementation) && !allowed.Device {
		return false
	}
	requirement := contract.Platform
	if requirement.OS != "" && requirement.OS != platform.OS || requirement.Arch != "" && requirement.Arch != platform.Arch {
		return false
	}
	features := make(map[string]struct{}, len(platform.Features))
	for _, feature := range platform.Features {
		features[feature] = struct{}{}
	}
	for _, feature := range requirement.Features {
		if _, ok := features[feature]; !ok {
			return false
		}
	}
	return true
}

func codecCandidateMatches(index catalog.Index, candidate plugin.Identity, input stream.Descriptor) bool {
	tag, tagged := codec.TagOf(input.Properties())
	if !tagged {
		return true
	}
	bindings := index.CodecBindings(candidate)
	if len(bindings) == 0 {
		return true
	}
	for _, binding := range bindings {
		if binding.Tag() == tag {
			return true
		}
	}
	return false
}

type candidateResult struct {
	bridge      bridge
	config      config.ResolvedView
	compilation plugin.Compilation
	output      stream.Descriptor
}

type compileKey struct {
	component   plugin.Identity
	config      config.Fingerprint
	input       stream.Fingerprint
	environment string
}

type compileEntry struct {
	input  stream.Descriptor
	result candidateResult
	err    error
}

type compileCache map[compileKey][]compileEntry

func (p *planner) compileBridge(candidate bridge, resolved config.ResolvedView, input stream.Descriptor) (candidateResult, error) {
	shape, err := candidate.component.Shape(plugin.ShapeContext{}, resolved)
	if err != nil {
		return candidateResult{}, rejectError{code: "shape"}
	}
	if len(shape.Inputs) != 1 || len(shape.Outputs) != 1 || shape.Inputs[0].Multiplicity() != flow.One || shape.Outputs[0].Multiplicity() != flow.One || shape.Inputs[0].Schema().Identity() != input.Schema() {
		return candidateResult{}, rejectError{code: "shape"}
	}
	fingerprint, err := input.Fingerprint()
	if err != nil {
		return candidateResult{}, rejectError{code: "descriptor"}
	}
	key := compileKey{component: candidate.component.Identity(), config: resolved.Fingerprint, input: fingerprint, environment: p.environment}
	for _, entry := range p.cache[key] {
		if entry.input.SameState(input) {
			p.usage.CacheHits++
			return entry.result, entry.err
		}
	}
	if err := p.beforeCompile(); err != nil {
		return candidateResult{}, err
	}
	compileContext := plugin.CompileContextWithContext(plugin.CompileContext{}, p.context)
	compilation, compileErr := plugin.Compile(candidate.component, compileContext, resolved, flow.NewDescriptors(flow.Describe(shape.Inputs[0].ID(), input)))
	result := candidateResult{bridge: bridge{component: candidate.component, input: shape.Inputs[0], output: shape.Outputs[0]}, config: resolved, compilation: compilation}
	if compileErr == nil {
		result.output, compileErr = validateBridgeResult(compilation, shape.Outputs[0], input, p.policy)
	}
	if compileErr != nil {
		compileErr = rejectError{code: rejectionCode(compileErr)}
	}
	p.cache[key] = append(p.cache[key], compileEntry{input: input, result: result, err: compileErr})
	return result, compileErr
}

func validateBridgeResult(compilation plugin.Compilation, outputPort flow.Port, input stream.Descriptor, policy job.Policy) (stream.Descriptor, error) {
	requirements, ok := plugin.RequirementsOf[stream.Descriptor](compilation)
	if !ok || len(requirements) != 0 {
		return stream.Descriptor{}, rejectError{code: "requirement"}
	}
	outputs, ok := plugin.OutputsOf[stream.Descriptor](compilation)
	if !ok {
		return stream.Descriptor{}, rejectError{code: "output-type"}
	}
	output, one := outputs.One(outputPort.ID())
	if !one || !output.Valid() || output.Schema() != outputPort.Schema().Identity() {
		return stream.Descriptor{}, rejectError{code: "output"}
	}
	if output.ID() != input.ID() {
		return stream.Descriptor{}, rejectError{code: "stream-identity"}
	}
	if output.Metadata().Scope() != input.Metadata().Scope() {
		return stream.Descriptor{}, rejectError{code: "metadata-scope"}
	}
	effects := compilation.Effects()
	if len(effects) == 0 {
		return stream.Descriptor{}, rejectError{code: "effect-missing"}
	}
	for _, effect := range effects {
		switch effect.Kind {
		case plugin.StructuralEffect, plugin.RepresentationEffect, plugin.CompressionEffect:
		default:
			return stream.Descriptor{}, rejectError{code: "effect-forbidden"}
		}
	}
	if policy.Resources.Limited && !policy.Resources.Limit.Satisfies(compilation.Resources()) {
		return stream.Descriptor{}, rejectError{code: "resource"}
	}
	return output, nil
}

func (p *planner) configs(candidate bridge, input stream.Descriptor, need plugin.Need[stream.Descriptor], remaining int) ([]config.ResolvedView, int, bool, error) {
	values := make([]config.ResolvedView, 0, 1)
	seen := make(map[config.Fingerprint]struct{})
	resolved, err := candidate.component.Resolve(config.NewPatch())
	if err == nil {
		values = append(values, resolved)
		seen[resolved.Fingerprint] = struct{}{}
	}
	if err := p.checkContext(); err != nil {
		return nil, 0, false, err
	}
	suggested, err := plugin.Suggest(candidate.component, plugin.SuggestContext{}, input, need)
	if err != nil {
		return values, 0, false, rejectError{code: rejectionCode(err)}
	}
	sort.Slice(suggested, func(left, right int) bool {
		return suggested[left].Fingerprint.String() < suggested[right].Fingerprint.String()
	})
	limited := len(suggested) > remaining
	if limited {
		suggested = suggested[:remaining]
	}
	suggestionCount := len(suggested)
	p.usage.Suggestions += suggestionCount
	for _, value := range suggested {
		if _, exists := seen[value.Fingerprint]; exists {
			continue
		}
		seen[value.Fingerprint] = struct{}{}
		values = append(values, value)
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].Fingerprint.String() < values[right].Fingerprint.String()
	})
	return values, suggestionCount, limited, nil
}

func environmentFingerprint(policy job.Policy, platform plan.Platform) string {
	canonical, _ := json.Marshal(struct {
		Policy   job.Policy
		Platform plan.Platform
	}{Policy: policy, Platform: platform})
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

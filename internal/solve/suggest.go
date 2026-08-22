package solve

import (
	"sort"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plugin"
)

func (p *planner) bridgeConfigs(candidate bridge, input stream.Descriptor, need plugin.Need[stream.Descriptor], remaining int) ([]config.ResolvedView, int, bool, error) {
	defaults := make([]config.ResolvedView, 0, 1)
	resolved, err := candidate.component.Resolve(config.NewPatch())
	if err == nil {
		defaults = append(defaults, resolved)
	}
	inputs := flow.NewDescriptors(flow.Describe(candidate.input.ID(), input))
	suggestion := plugin.NewSuggestion(inputs, plugin.OutputDemand(candidate.output.ID(), need))
	suggested, count, limited, err := p.suggestedConfigs(candidate.component, suggestion, remaining, defaults...)
	if err != nil {
		return nil, count, limited, err
	}
	return append(defaults, suggested...), count, limited, nil
}

func (p *planner) fixedConfigs(component plugin.Component, inputs flow.Descriptors[stream.Descriptor], requirements []plugin.Requirement[stream.Descriptor], current config.ResolvedView, remaining int) ([]config.ResolvedView, int, bool, error) {
	demands := make([]plugin.Demand[stream.Descriptor], 0, len(requirements))
	for _, requirement := range requirements {
		demands = append(demands, plugin.InputDemand(requirement.Port(), requirement.Need()))
	}
	sort.Slice(demands, func(left, right int) bool {
		return demandOrder(demands[left]) < demandOrder(demands[right])
	})
	return p.suggestedConfigs(component, plugin.NewSuggestion(inputs, demands...), remaining, current)
}

func demandOrder(demand plugin.Demand[stream.Descriptor]) string {
	key := demand.Port() + "\x00" + demand.Need().Code()
	desired, ok := demand.Need().Desired()
	if !ok {
		return key
	}
	fingerprint, err := desired.Fingerprint()
	if err != nil {
		return key
	}
	return key + "\x00" + fingerprint.String()
}

// suggestedConfigs canonicalizes component-owned suggestions into planner
// patches. Excluded values were already compiled by the current search state,
// so returning one cannot make planning progress.
func (p *planner) suggestedConfigs(component plugin.Component, suggestion plugin.Suggestion[stream.Descriptor], remaining int, excluded ...config.ResolvedView) ([]config.ResolvedView, int, bool, error) {
	if err := p.checkContext(); err != nil {
		return nil, 0, false, err
	}
	values, err := plugin.Suggest(component, plugin.SuggestContext{}, suggestion)
	if err != nil {
		return nil, 0, false, rejectError{code: rejectionCode(err)}
	}
	sort.Slice(values, func(left, right int) bool {
		return values[left].Fingerprint().String() < values[right].Fingerprint().String()
	})
	limited := len(values) > remaining
	if limited {
		values = values[:remaining]
	}
	p.usage.Suggestions += len(values)
	seen := make(map[config.Fingerprint]struct{}, len(excluded)+len(values))
	for _, value := range excluded {
		seen[value.Fingerprint()] = struct{}{}
	}
	result := make([]config.ResolvedView, 0, len(values))
	for _, value := range values {
		patch, patchErr := component.Schema().Patch(value)
		if patchErr != nil {
			return nil, 0, false, rejectError{code: rejectionCode(patchErr)}
		}
		planned, resolveErr := component.Resolve(patch.Planned())
		if resolveErr != nil {
			return nil, 0, false, rejectError{code: rejectionCode(resolveErr)}
		}
		if _, exists := seen[planned.Fingerprint()]; exists {
			continue
		}
		seen[planned.Fingerprint()] = struct{}{}
		result = append(result, planned)
	}
	return result, len(values), limited, nil
}

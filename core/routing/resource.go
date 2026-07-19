package routing

import "github.com/godexture/core/registry"

func allocateResources(requests []registry.ResourceRequest, parallelism int) []registry.ResourceBudget {
	allocations := make([]registry.ResourceBudget, len(requests))
	parallelStages := 0
	for _, request := range requests {
		if request.Parallelism {
			parallelStages++
		}
	}
	if parallelStages == 0 {
		return allocations
	}

	perStage := parallelism / parallelStages
	remainder := parallelism % parallelStages
	if perStage < 1 {
		perStage = 1
		remainder = 0
	}
	for i, request := range requests {
		if !request.Parallelism {
			continue
		}
		allocations[i].Parallelism = perStage
		if remainder > 0 {
			allocations[i].Parallelism++
			remainder--
		}
	}
	return allocations
}

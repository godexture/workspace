package routing

import "github.com/godexture/godec/core/registry"

// grantResources hands every parallel-eligible request a reference to the
// same shared pool, so idle stages never hold capacity a busy stage could
// use instead. Non-parallel requests get a zero-value grant (no pool).
func grantResources(requests []registry.ResourceRequest, pool *registry.WorkerPool) []registry.ResourceGrant {
	grants := make([]registry.ResourceGrant, len(requests))
	if pool == nil {
		return grants
	}
	for i, request := range requests {
		if request.Parallelism {
			grants[i].Pool = pool
		}
	}
	return grants
}

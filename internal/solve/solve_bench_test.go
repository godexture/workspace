package solve

import (
	"context"
	"strconv"
	"testing"

	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/plugin"
)

func BenchmarkResolve(b *testing.B) {
	tests := []struct {
		name       string
		target     schema.Type[solveUnit]
		components func() []plugin.Component
	}{
		{
			name:   "simple-copy",
			target: solveSchemaA,
			components: func() []plugin.Component {
				return nil
			},
		},
		{
			name:   "one-bridge",
			target: solveSchemaB,
			components: func() []plugin.Component {
				return []plugin.Component{solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)}
			},
		},
		{
			name:   "long-bridge",
			target: solveSchemaD,
			components: func() []plugin.Component {
				return []plugin.Component{
					solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil),
					solveBridge[solveBridgeBCID](solveSchemaB, solveSchemaC, structural("bc"), schemaTransform(solveSchemaC), nil, 0, plugin.Contract{}, nil, nil),
					solveBridge[solveBridgeCDID](solveSchemaC, solveSchemaD, structural("cd"), schemaTransform(solveSchemaD), nil, 0, plugin.Contract{}, nil, nil),
				}
			},
		},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			source := solveSource(solveSchemaA, nil)
			sink := solveSink(test.target, false, nil)
			components := append([]plugin.Component{source, sink}, test.components()...)
			index := solveIndex(b, components...)
			request := solveRequest(b, source, sink, job.DefaultBudget())
			b.ReportAllocs()
			var compiles, states int
			for iteration := 0; iteration < b.N; iteration++ {
				program, err := Resolve(context.Background(), index, request, solvePlatform())
				if err != nil {
					b.Fatal(err)
				}
				usage := program.Plan().Usage()
				compiles += usage.Compiles
				states += usage.States
			}
			if b.N != 0 {
				b.ReportMetric(float64(compiles)/float64(b.N), "compiles/op")
				b.ReportMetric(float64(states)/float64(b.N), "states/op")
			}
		})
	}
}

var benchmarkIndexResult int

func BenchmarkCandidateIndexScale(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 10000} {
		b.Run("catalog-"+strconv.Itoa(size), func(b *testing.B) {
			index := make(candidateIndex, size)
			for component := 0; component < size-1; component++ {
				index["unrelated-"+strconv.Itoa(component)] = nil
			}
			index[solveSchemaA.Identity().String()] = []bridge{{}}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				benchmarkIndexResult = len(index[solveSchemaA.Identity().String()])
			}
			b.ReportMetric(float64(size), "components")
		})
	}
}

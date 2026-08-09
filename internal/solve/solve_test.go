package solve

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/job"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/plan"
	"github.com/godexture/godec/plugin"
)

func TestResolveDirectGraphProducesRequestedProgramWithoutOpening(t *testing.T) {
	var opened atomic.Int32
	source := solveSource(solveSchemaA, &opened)
	sink := solveSink(solveSchemaA, false, &opened)
	program, err := Resolve(context.Background(), solveIndex(t, source, sink), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	if !program.Valid() || opened.Load() != 0 {
		t.Fatalf("program valid=%v opened=%d", program.Valid(), opened.Load())
	}
	nodes := program.Plan().Nodes()
	if len(nodes) != 2 || nodes[0].Origin != plan.Requested || nodes[1].Origin != plan.Requested || len(program.Plan().Edges()) != 1 || program.Plan().Edges()[0].Origin != plan.Requested {
		t.Fatalf("direct Plan nodes=%#v edges=%#v", nodes, program.Plan().Edges())
	}
	operator, err := program.Open(plugin.NewOpenContext(context.Background(), plugin.OpenServices{}), "source")
	if err != nil {
		t.Fatal(err)
	}
	if err := operator.Close(); err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 1 {
		t.Fatalf("Program.Open count = %d", opened.Load())
	}
}

func TestResolveInsertsOneBridgeAndExplainsIt(t *testing.T) {
	var opened atomic.Int32
	source := solveSource(solveSchemaA, &opened)
	sink := solveSink(solveSchemaB, false, &opened)
	bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("parse"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, &opened, nil)
	program, err := Resolve(context.Background(), solveIndex(t, source, sink, bridge), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 0 {
		t.Fatalf("planning opened %d operators", opened.Load())
	}
	var automatic plan.Node
	for _, node := range program.Plan().Nodes() {
		if node.Origin == plan.Automatic {
			automatic = node
		}
	}
	if automatic.ID == "" || automatic.Component != bridge.Identity().String() || automatic.Reason != "graph.schema-mismatch" || len(automatic.Inputs) != 1 || len(automatic.Outputs) != 1 {
		t.Fatalf("automatic node = %#v", automatic)
	}
	if len(program.Plan().Edges()) != 2 {
		t.Fatalf("automatic edges = %#v", program.Plan().Edges())
	}
	for _, edge := range program.Plan().Edges() {
		if edge.Origin != plan.Automatic || edge.Reason != "graph.schema-mismatch" {
			t.Fatalf("edge explanation = %#v", edge)
		}
	}
	operator, err := program.Open(plugin.NewOpenContext(context.Background(), plugin.OpenServices{}), job.NodeID(automatic.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	if opened.Load() != 1 {
		t.Fatalf("selected bridge open count = %d", opened.Load())
	}
}

func TestResolveFindsLongPathAndIgnoresUnrelatedCandidates(t *testing.T) {
	var unrelatedCompiles atomic.Int32
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaD, false, nil)
	ab := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	bc := solveBridge[solveBridgeBCID](solveSchemaB, solveSchemaC, structural("bc"), schemaTransform(solveSchemaC), nil, 0, plugin.Contract{}, nil, nil)
	cd := solveBridge[solveBridgeCDID](solveSchemaC, solveSchemaD, structural("cd"), schemaTransform(solveSchemaD), nil, 0, plugin.Contract{}, nil, nil)
	unrelated := solveBridge[solveUnrelatedID](solveSchemaD, solveSchemaA, structural("unrelated"), schemaTransform(solveSchemaA), nil, 0, plugin.Contract{}, nil, &unrelatedCompiles)
	program, err := Resolve(context.Background(), solveIndex(t, source, sink, unrelated, cd, bc, ab), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	var components []string
	for _, node := range program.Plan().Nodes() {
		if node.Origin == plan.Automatic {
			components = append(components, node.Component)
		}
	}
	if !reflect.DeepEqual(components, []string{ab.Identity().String(), bc.Identity().String(), cd.Identity().String()}) {
		t.Fatalf("automatic path = %v", components)
	}
	if unrelatedCompiles.Load() != 0 {
		t.Fatalf("unrelated candidate compiled %d times", unrelatedCompiles.Load())
	}
}

func TestResolveConditionGapSkipsNonProgressCandidate(t *testing.T) {
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaA, true, nil)
	nonProgress := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaA, structural("identity"), identityTransform, nil, 0, plugin.Contract{}, nil, nil)
	resample := solveBridge[solveBridgeAAID](solveSchemaA, solveSchemaA, plugin.Effect{Kind: plugin.RepresentationEffect, Loss: plugin.BoundedLoss, Detail: "time-base"}, timeBaseTransform, nil, 0, plugin.Contract{}, nil, nil)
	program, err := Resolve(context.Background(), solveIndex(t, source, sink, nonProgress, resample), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	var automatic []string
	for _, node := range program.Plan().Nodes() {
		if node.Origin == plan.Automatic {
			automatic = append(automatic, node.Component)
		}
	}
	if !reflect.DeepEqual(automatic, []string{resample.Identity().String()}) {
		t.Fatalf("condition path = %v", automatic)
	}
}

func TestResolveUsesLexicographicEffectRankBeforeIdentity(t *testing.T) {
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaB, false, nil)
	worse := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, plugin.Effect{Kind: plugin.CompressionEffect, Loss: plugin.Lossy, Detail: "lossy"}, schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	better := solveBridge[solveBridgeABSecondID](solveSchemaA, solveSchemaB, structural("structural"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	program, err := Resolve(context.Background(), solveIndex(t, source, sink, worse, better), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range program.Plan().Nodes() {
		if node.Origin == plan.Automatic && node.Component != better.Identity().String() {
			t.Fatalf("selected component = %s, want %s", node.Component, better.Identity())
		}
	}
}

func TestResolveConvergesRequestedMultiInputNode(t *testing.T) {
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaB, false, nil)
	bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	mixerShape := flow.NewShape(
		[]flow.Port{flow.In("left", solveSchemaB), flow.In("right", solveSchemaB)},
		[]flow.Port{flow.Out("out", solveSchemaB)},
	)
	mixer := solveComponent[solveMixerID](mixerShape, func(_ solveConfig, inputs flow.Descriptors[stream.Descriptor]) plugin.Compiled[solvePlan, stream.Descriptor] {
		left, leftOK := inputs.One("left")
		_, rightOK := inputs.One("right")
		if !leftOK || !rightOK {
			return plugin.Compiled[solvePlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("left", plugin.ConditionNeed[stream.Descriptor]("fixture.inputs"))}}
		}
		return plugin.Compiled[solvePlan, stream.Descriptor]{Outputs: flow.NewDescriptors(flow.Describe("out", left))}
	}, nil, 0, plugin.Contract{}, nil, nil)
	requested, err := job.NewGraph(
		[]job.Node{
			job.NewNode("left-source", source.Identity(), config.NewPatch()),
			job.NewNode("right-source", source.Identity(), config.NewPatch()),
			job.NewNode("mixer", mixer.Identity(), config.NewPatch()),
			job.NewNode("sink", sink.Identity(), config.NewPatch()),
		},
		[]job.Edge{
			job.Connect(job.At("left-source", "out"), job.At("mixer", "left")),
			job.Connect(job.At("right-source", "out"), job.At("mixer", "right")),
			job.Connect(job.At("mixer", "out"), job.At("sink", "in")),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := job.New(nil, nil, requested)
	if err != nil {
		t.Fatal(err)
	}
	program, err := Resolve(context.Background(), solveIndex(t, source, sink, bridge, mixer), request, solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	automatic := 0
	for _, node := range program.Plan().Nodes() {
		if node.Origin == plan.Automatic {
			automatic++
		}
	}
	if automatic != 2 || program.Plan().Usage().FixpointIterations != 3 || program.Plan().Usage().CacheHits == 0 {
		t.Fatalf("automatic=%d usage=%#v", automatic, program.Plan().Usage())
	}
}

func TestResolveHonorsCallerCancellation(t *testing.T) {
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaA, false, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Resolve(ctx, solveIndex(t, source, sink), solveRequest(t, source, sink, job.DefaultBudget()), solvePlatform())
	assertSolveCode(t, err, "solve.canceled")
}

func TestResolveRejectsForbiddenEffectsAndIncompatibleContracts(t *testing.T) {
	tests := []struct {
		name     string
		effect   plugin.Effect
		policy   func(job.Policy) job.Policy
		contract plugin.Contract
	}{
		{name: "content", effect: plugin.Effect{Kind: plugin.ContentEffect, Loss: plugin.NoLoss, Detail: "artistic"}},
		{name: "accuracy", effect: structural("bounded"), policy: func(value job.Policy) job.Policy { value.Accuracy = job.ExactAccuracy; return value }, contract: plugin.Contract{Accuracy: plugin.BoundedContract, Repeatability: plugin.RepeatableContract, Artifact: plugin.NoArtifactSupport, Implementation: plugin.PureGoImplementation}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := solveSource(solveSchemaA, nil)
			sink := solveSink(solveSchemaB, false, nil)
			bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, test.effect, schemaTransform(solveSchemaB), nil, 0, test.contract, nil, nil)
			request := solveRequest(t, source, sink, job.DefaultBudget())
			if test.policy != nil {
				graph, _ := request.Graph()
				policy := test.policy(request.Policy())
				var err error
				request, err = job.New(nil, nil, graph, job.WithBudget(request.Budget()), job.WithPolicy(policy))
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := Resolve(context.Background(), solveIndex(t, source, sink, bridge), request, solvePlatform())
			assertSolveCode(t, err, "solve.unsupported")
		})
	}
}

func TestResolveDistinguishesPlannerBudgets(t *testing.T) {
	suggest := func(plugin.SuggestContext, stream.Descriptor, plugin.Need[stream.Descriptor]) []solveConfig {
		return []solveConfig{{Mode: 2}, {Mode: 1}}
	}
	tests := []struct {
		name      string
		budget    job.Budget
		suggest   plugin.SuggestFunc[solveConfig, stream.Descriptor]
		limit     int
		dimension string
	}{
		{name: "states", budget: job.Budget{States: 1, Compiles: 20, SuggestionsPerNeed: 4, FixpointIterations: 4}, dimension: "states"},
		{name: "compiles", budget: job.Budget{States: 20, Compiles: 1, SuggestionsPerNeed: 4, FixpointIterations: 4}, dimension: "compiles"},
		{name: "suggestions", budget: job.Budget{States: 20, Compiles: 20, SuggestionsPerNeed: 1, FixpointIterations: 4}, suggest: suggest, limit: 2, dimension: "suggestions"},
		{name: "fixpoints", budget: job.Budget{States: 20, Compiles: 20, SuggestionsPerNeed: 4, FixpointIterations: 1}, dimension: "fixpoints"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := solveSource(solveSchemaA, nil)
			sink := solveSink(solveSchemaB, false, nil)
			bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), test.suggest, test.limit, plugin.Contract{}, nil, nil)
			_, err := Resolve(context.Background(), solveIndex(t, source, sink, bridge), solveRequest(t, source, sink, test.budget), solvePlatform())
			items := diagnostic.ItemsOf(err)
			if len(items) != 1 || items[0].Code != "solve.budget-exhausted" || items[0].Detail["dimension"] != test.dimension {
				t.Fatalf("budget diagnostic = %#v", items)
			}
		})
	}
}

func TestResolveIsIndependentOfCatalogAndSuggestionOrder(t *testing.T) {
	source := solveSource(solveSchemaA, nil)
	sink := solveSink(solveSchemaB, false, nil)
	firstBridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	secondBridge := solveBridge[solveBridgeABSecondID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	request := solveRequest(t, source, sink, job.DefaultBudget())
	first, err := Resolve(context.Background(), solveIndex(t, source, sink, secondBridge, firstBridge), request, solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Resolve(context.Background(), solveIndex(t, firstBridge, source, secondBridge, sink), request, solvePlatform())
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan().Fingerprint() != second.Plan().Fingerprint() || first.Plan().ExecutionSignature() != second.Plan().ExecutionSignature() {
		t.Fatal("catalog insertion order changed Plan identity")
	}

	orders := [][]solveConfig{{{Mode: 2}, {Mode: 1}}, {{Mode: 1}, {Mode: 2}}}
	var fingerprints []plan.Fingerprint
	for _, values := range orders {
		values := append([]solveConfig(nil), values...)
		suggest := func(plugin.SuggestContext, stream.Descriptor, plugin.Need[stream.Descriptor]) []solveConfig {
			return append([]solveConfig(nil), values...)
		}
		bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), suggest, 2, plugin.Contract{}, nil, nil)
		program, err := Resolve(context.Background(), solveIndex(t, source, sink, bridge), request, solvePlatform())
		if err != nil {
			t.Fatal(err)
		}
		fingerprints = append(fingerprints, program.Plan().Fingerprint())
	}
	if fingerprints[0] != fingerprints[1] {
		t.Fatal("Suggest enumeration order changed Plan fingerprint")
	}
}

func TestControlPlaneComponentIsNotASolverCandidate(t *testing.T) {
	bridge := solveBridge[solveBridgeABID](solveSchemaA, solveSchemaB, structural("ab"), schemaTransform(solveSchemaB), nil, 0, plugin.Contract{}, nil, nil)
	index := solveIndex(t, bridge, solveControlComponent())
	policy, ok := job.PolicyFor(job.Fast)
	if !ok {
		t.Fatal("Fast policy is unavailable")
	}
	candidates := buildCandidateIndex(index, policy, solvePlatform())
	foundBridge := false
	for _, values := range candidates {
		for _, candidate := range values {
			if candidate.component.Identity() == plugin.IdentityOf[solveControlID]() {
				t.Fatal("control-plane component entered solver candidate index")
			}
			foundBridge = foundBridge || candidate.component.Identity() == bridge.Identity()
		}
	}
	if !foundBridge {
		t.Fatal("executable bridge is absent from solver candidate index")
	}
}

func assertSolveCode(t *testing.T, err error, code string) {
	t.Helper()
	items := diagnostic.ItemsOf(err)
	if len(items) == 0 || items[0].Code != code {
		t.Fatalf("diagnostic = %v, want %s", err, code)
	}
}

package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/flow"
	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/metadata/loss"
	"github.com/godexture/godec/media/property"
	"github.com/godexture/godec/media/schema"
	"github.com/godexture/godec/media/stream"
	"github.com/godexture/godec/media/timing"
	"github.com/godexture/godec/plugin"
)

type metadataAnchorPluginID struct{}
type metadataAnchorConfigID struct{}
type metadataAnchorSchemaID struct{}
type metadataAnchorComponentID struct{}

type metadataAnchorConfig struct{}
type metadataAnchorPlan struct{ shape flow.Shape }

var metadataAnchorType = schema.Define[metadataAnchorSchemaID](schema.Traits[int]{})

type metadataEvaluator struct {
	resolver metadata.Resolver
	input    MetadataFixture
	want     MetadataExpectation
	config   string
}

type metadataOutcome struct {
	document metadata.Document
	payload  metadata.Blob
	lost     []loss.Report
	err      error
	panicErr error
}

func newMetadataScenario(subject MetadataSubject, test MetadataCase) (*scenarioCore, error) {
	component, ok := componentOf(subject.set, subject.identity)
	if !ok {
		return nil, fmt.Errorf("Metadata subject %s is absent", subject.identity)
	}
	resolved, err := component.Resolve(test.Config)
	if err != nil {
		return nil, err
	}
	mappings, err := metadataMappings(subject.set)
	if err != nil {
		return nil, err
	}
	resolver, err := metadata.NewResolver(map[carrier.ID]plugin.Component{test.Input.carrier: component}, mappings)
	if err != nil {
		return nil, err
	}
	set := subject.set.Add(metadataAnchorDefinition())
	anchor := SubjectIn(set, plugin.IdentityOf[metadataAnchorComponentID](), "in", metadataAnchorType, "out", metadataAnchorType)
	descriptor := stream.MustDescriptor("testkit-metadata", metadataAnchorType.Descriptor(), timing.Base{}, property.New())
	base, err := newScenario(componentRunner, anchor, config.NewPatch(), Values(descriptor, metadataAnchorType, 1), EqualValues(1).newRecorder())
	if err != nil {
		return nil, err
	}
	evaluator := metadataEvaluator{
		resolver: resolver,
		input:    test.Input,
		want:     test.Want,
		config:   resolved.Fingerprint().String(),
	}
	anchorFinish := base.finish
	base.selected = anchor.identity
	base.purity = func(ctx context.Context) (string, error) {
		outcome := evaluator.evaluate(ctx)
		if outcome.panicErr != nil {
			return "", outcome.panicErr
		}
		return outcome.signature(evaluator.config)
	}
	base.cancelCheck = func() error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		outcome := evaluator.evaluate(ctx)
		return outcome.panicErr
	}
	base.finish = func() error {
		return errors.Join(anchorFinish(), evaluator.verify(evaluator.evaluate(context.Background())))
	}
	return base, nil
}

func (e metadataEvaluator) evaluate(ctx context.Context) metadataOutcome {
	document, parseErr, panicErr := safeMetadataParse(func() (metadata.Document, error) {
		return e.resolver.Parse(ctx, e.input.carrier, e.input.block, e.input.scope, e.input.payload)
	})
	if panicErr != nil || parseErr != nil {
		return metadataOutcome{err: parseErr, panicErr: panicErr}
	}
	var lost []loss.Report
	payload, marshalErr, panicErr := safeMetadataMarshal(func() (metadata.Blob, error) {
		value, values, err := e.resolver.Marshal(ctx, e.input.carrier, e.input.block, metadata.MustAvailable(document))
		lost = values
		return value, err
	})
	return metadataOutcome{document: document, payload: payload, lost: lost, err: marshalErr, panicErr: panicErr}
}

func (e metadataEvaluator) verify(outcome metadataOutcome) error {
	if outcome.panicErr != nil {
		return outcome.panicErr
	}
	if e.want.failureCode != "" {
		if outcome.err == nil {
			return fmt.Errorf("Metadata Encoding succeeded, want diagnostic %q", e.want.failureCode)
		}
		if !hasDiagnostic(outcome.err, e.want.failureCode) {
			return fmt.Errorf("Metadata diagnostics = %v, want %q", diagnostic.ItemsOf(outcome.err), e.want.failureCode)
		}
		return nil
	}
	if outcome.err != nil {
		return outcome.err
	}
	if err := compareMetadataDocuments(outcome.document, e.want.document); err != nil {
		return err
	}
	if !outcome.payload.Equal(e.want.payload) {
		return fmt.Errorf("Metadata payload = %x (%s), want %x (%s)", outcome.payload.AppendTo(nil), outcome.payload.MediaType(), e.want.payload.AppendTo(nil), e.want.payload.MediaType())
	}
	if !reflect.DeepEqual(outcome.lost, e.want.reports) {
		return fmt.Errorf("Metadata reports = %#v, want %#v", outcome.lost, e.want.reports)
	}
	return nil
}

func safeMetadataParse(call func() (metadata.Document, error)) (document metadata.Document, err error, panicErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("Metadata Parse panicked: %s\n%s", diagnostic.Recovered(recovered), debug.Stack())
			document = metadata.Document{}
			err = nil
		}
	}()
	document, err = call()
	return document, err, nil
}

func safeMetadataMarshal(call func() (metadata.Blob, error)) (payload metadata.Blob, err error, panicErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = fmt.Errorf("Metadata Marshal panicked: %s\n%s", diagnostic.Recovered(recovered), debug.Stack())
			payload = metadata.Blob{}
			err = nil
		}
	}()
	payload, err = call()
	return payload, err, nil
}

type metadataDocumentSnapshot struct {
	Scope   string                  `json:"scope"`
	Entries []metadataEntrySnapshot `json:"entries"`
	Blocks  []metadataBlockSnapshot `json:"blocks"`
}

type metadataEntrySnapshot struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Encoding string `json:"encoding"`
	Carrier  string `json:"carrier"`
	Block    string `json:"block"`
	Native   string `json:"native"`
}

type metadataBlockSnapshot struct {
	ID        string `json:"id"`
	Carrier   string `json:"carrier"`
	Encoding  string `json:"encoding"`
	Source    bool   `json:"source"`
	MediaType string `json:"mediaType"`
	Payload   []byte `json:"payload"`
}

type metadataPuritySnapshot struct {
	Config   string                   `json:"config"`
	Document metadataDocumentSnapshot `json:"document"`
	Payload  metadataBlockSnapshot    `json:"payload"`
	Reports  []metadataLossSnapshot   `json:"reports"`
	Error    []string                 `json:"error"`
}

type metadataLossSnapshot struct {
	Carrier        string `json:"carrier"`
	Encoding       string `json:"encoding"`
	Block          string `json:"block"`
	Key            string `json:"key"`
	Kind           string `json:"kind"`
	Native         string `json:"native"`
	Mapping        string `json:"mapping"`
	Target         string `json:"target"`
	Detail         string `json:"detail"`
	SourceCarrier  string `json:"sourceCarrier"`
	SourceEncoding string `json:"sourceEncoding"`
	SourceBlock    string `json:"sourceBlock"`
	SourceNative   string `json:"sourceNative"`
}

func (o metadataOutcome) signature(configFingerprint string) (string, error) {
	snapshot := metadataPuritySnapshot{Config: configFingerprint}
	if o.err != nil {
		for _, item := range diagnostic.ItemsOf(o.err) {
			snapshot.Error = append(snapshot.Error, item.Code+":"+item.Message)
		}
		snapshot.Error = append(snapshot.Error, "error:"+o.err.Error())
	} else {
		snapshot.Document = snapshotMetadataDocument(o.document)
		snapshot.Payload = metadataBlockSnapshot{MediaType: o.payload.MediaType(), Payload: o.payload.AppendTo(nil)}
		snapshot.Reports = snapshotMetadataLosses(o.lost)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func snapshotMetadataLosses(values []loss.Report) []metadataLossSnapshot {
	result := make([]metadataLossSnapshot, len(values))
	for index, value := range values {
		entry := value.Loss
		result[index] = metadataLossSnapshot{
			Carrier: value.Carrier.String(), Encoding: value.Encoding, Block: value.Block,
			Key: entry.Key.String(), Kind: entry.Kind.String(), Native: entry.Native,
			Mapping: entry.Mapping.String(), Target: entry.Target.String(), Detail: entry.Detail,
			SourceCarrier: entry.Source.Carrier.String(), SourceEncoding: entry.Source.Encoding,
			SourceBlock: entry.Source.Block, SourceNative: entry.Source.Native,
		}
	}
	return result
}

func metadataMappings(set plugin.Set) ([]metadata.Mapping, error) {
	var result []metadata.Mapping
	for _, component := range set.Components() {
		mappings, ok := metadata.MappingsOf(component)
		if !ok {
			continue
		}
		if !mappings.Valid() {
			if problem := mappings.Problem(); problem != nil {
				return nil, fmt.Errorf("Metadata Mapping trait on %s is invalid: %w", component.Identity(), problem)
			}
			return nil, fmt.Errorf("Metadata Mapping trait on %s is invalid", component.Identity())
		}
		result = append(result, mappings.Values()...)
	}
	return result, nil
}

func snapshotMetadataDocument(document metadata.Document) metadataDocumentSnapshot {
	result := metadataDocumentSnapshot{Scope: document.Scope().String()}
	for _, entry := range document.Entries() {
		origin := entry.Origin()
		result.Entries = append(result.Entries, metadataEntrySnapshot{
			Key: entry.Key().String(), Type: entry.ValueType().String(), Value: fmt.Sprintf("%#v", entry.Value()),
			Encoding: origin.Encoding.String(), Carrier: origin.Carrier.String(), Block: string(origin.Block), Native: origin.Native,
		})
	}
	for _, block := range document.Blocks() {
		result.Blocks = append(result.Blocks, metadataBlockSnapshot{
			ID: string(block.ID()), Carrier: block.Carrier().String(), Encoding: block.Encoding().String(),
			Source: block.Source(), MediaType: block.Payload().MediaType(), Payload: block.Payload().AppendTo(nil),
		})
	}
	return result
}

func compareMetadataDocuments(actual, expected metadata.Document) error {
	if actual.Scope() != expected.Scope() {
		return fmt.Errorf("Metadata scope = %s, want %s", actual.Scope(), expected.Scope())
	}
	actualEntries := actual.Entries()
	expectedEntries := expected.Entries()
	if len(actualEntries) != len(expectedEntries) {
		return fmt.Errorf("Metadata entry count = %d, want %d", len(actualEntries), len(expectedEntries))
	}
	for index := range actualEntries {
		left, right := actualEntries[index], expectedEntries[index]
		if left.Key() != right.Key() || left.ValueType() != right.ValueType() || left.Origin() != right.Origin() || !reflect.DeepEqual(left.Value(), right.Value()) {
			return fmt.Errorf("Metadata entry %d = %#v/%#v/%#v, want %#v/%#v/%#v", index, left.Key(), left.Value(), left.Origin(), right.Key(), right.Value(), right.Origin())
		}
	}
	actualBlocks := actual.Blocks()
	expectedBlocks := expected.Blocks()
	if len(actualBlocks) != len(expectedBlocks) {
		return fmt.Errorf("Metadata raw block count = %d, want %d", len(actualBlocks), len(expectedBlocks))
	}
	for index := range actualBlocks {
		left, right := actualBlocks[index], expectedBlocks[index]
		if left.ID() != right.ID() || left.Carrier() != right.Carrier() || left.Encoding() != right.Encoding() || left.Source() != right.Source() || !left.Payload().Equal(right.Payload()) {
			return fmt.Errorf("Metadata raw block %d differs", index)
		}
	}
	return nil
}

func metadataAnchorDefinition() plugin.Definition {
	configuration := config.Struct[metadataAnchorConfigID](func() metadataAnchorConfig { return metadataAnchorConfig{} }).Version("1").Build()
	shape := flow.NewShape([]flow.Port{flow.In("in", metadataAnchorType)}, []flow.Port{flow.Out("out", metadataAnchorType)})
	component := plugin.NewComponent[metadataAnchorComponentID](plugin.Descriptor{DisplayName: "testkit Metadata anchor"}, configuration,
		plugin.WithSpec(plugin.Spec[metadataAnchorConfig, metadataAnchorPlan, stream.Descriptor]{
			Ports: shape,
			Compile: func(_ plugin.CompileContext, _ metadataAnchorConfig, inputs flow.Descriptors[stream.Descriptor]) (plugin.Compiled[metadataAnchorPlan, stream.Descriptor], error) {
				input, ok := inputs.One("in")
				if !ok {
					return plugin.Compiled[metadataAnchorPlan, stream.Descriptor]{Requirements: []plugin.Requirement[stream.Descriptor]{plugin.Require("in", plugin.ConditionNeed[stream.Descriptor]("testkit.metadata.anchor"))}}, nil
				}
				return plugin.Compiled[metadataAnchorPlan, stream.Descriptor]{Plan: metadataAnchorPlan{shape: shape.Clone()}, Outputs: flow.NewDescriptors(flow.Describe("out", input))}, nil
			},
			Open: func(plugin.OpenContext, metadataAnchorPlan) (flow.Operator, error) {
				return metadataAnchorOperator{shape: shape.Clone()}, nil
			},
		}),
		plugin.WithProcessor("in", metadataAnchorType, "out", metadataAnchorType),
	)
	return plugin.Define[metadataAnchorPluginID](plugin.Descriptor{DisplayName: "testkit Metadata anchor", Version: "1"}, component)
}

type metadataAnchorOperator struct{ shape flow.Shape }

func (o metadataAnchorOperator) Ports() flow.Shape { return o.shape.Clone() }
func (metadataAnchorOperator) Close() error        { return nil }
func (metadataAnchorOperator) Process(ctx context.Context, input *flow.Item[int], output flow.Emitter[int]) error {
	return output.Emit(ctx, input)
}
func (metadataAnchorOperator) Flush(context.Context, flow.Emitter[int]) error { return nil }

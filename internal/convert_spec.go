package cli

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/godexture/sdk/catalog"
	"github.com/godexture/sdk/cliflag"
	"github.com/godexture/sdk/conversion"
)

// buildSpec turns convert flags into a conversion.Spec. --format infers the
// muxer from the output extension (via the catalog) when omitted; --codec
// carries both the target codec ID and the values applied to whichever
// encoder is resolved for it (CLI has no separate flag to pick an encoder
// plugin by name).
func buildSpec(options convertOptions, outputPath string, outputs []catalog.OutputFormat) (conversion.Spec, error) {
	format, formatParameters, err := parsePluginSpec(options.format)
	if err != nil {
		return conversion.Spec{}, fmt.Errorf("format: %w", err)
	}
	if err := rejectPluginParameters("format", formatParameters); err != nil {
		return conversion.Spec{}, err
	}
	if format == nil {
		name, inferErr := inferMuxerName(outputs, outputPath)
		if inferErr != nil {
			return conversion.Spec{}, inferErr
		}
		format = &conversion.PluginSpec{Name: name}
	}

	codec, codecParameters, err := parsePluginSpec(options.codec)
	if err != nil {
		return conversion.Spec{}, fmt.Errorf("codec: %w", err)
	}
	if err := rejectPluginParameters("codec", codecParameters); err != nil {
		return conversion.Spec{}, err
	}

	spec := conversion.Spec{Muxer: *format, Parallelism: options.jobs}
	if codec != nil {
		spec.Codec = codec.Name
		spec.Encoder = &conversion.PluginSpec{Values: codec.Values}
	}
	var demuxerParameters map[string]string
	if spec.Demuxer, demuxerParameters, err = parsePluginSpec(options.demuxer); err != nil {
		return conversion.Spec{}, fmt.Errorf("demuxer: %w", err)
	}
	if err := rejectPluginParameters("demuxer", demuxerParameters); err != nil {
		return conversion.Spec{}, err
	}
	var decoderParameters map[string]string
	if spec.Decoder, decoderParameters, err = parsePluginSpec(options.decoder); err != nil {
		return conversion.Spec{}, fmt.Errorf("decoder: %w", err)
	}
	if err := rejectPluginParameters("decoder", decoderParameters); err != nil {
		return conversion.Spec{}, err
	}
	// aliasIndex tracks every declared filter alias to its position in
	// spec.Filters, so --wire can look up its destination; aliasSeen also
	// covers auxiliary input names, since the two namespaces share one
	// alias space (see conversion.MainInputAlias and routing.MainInputAlias
	// for the two names reserved out of it).
	aliasIndex := make(map[string]int, len(options.filters))
	aliasSeen := make(map[string]bool, len(options.filters)+len(options.inputs))
	for _, value := range options.filters {
		alias, filter, parameters, parseErr := parseFilterSpec(value)
		if parseErr != nil {
			return conversion.Spec{}, fmt.Errorf("filter: %w", parseErr)
		}
		if alias == "" {
			alias = filter.Name
		}
		if !validGraphAlias(alias) {
			return conversion.Spec{}, fmt.Errorf("filter: invalid alias %q", alias)
		}
		if aliasSeen[alias] {
			return conversion.Spec{}, fmt.Errorf("filter: duplicate alias %q", alias)
		}
		aliasSeen[alias] = true
		aliasIndex[alias] = len(spec.Filters)
		spec.Filters = append(spec.Filters, conversion.FilterSpec{PluginSpec: *filter, Alias: alias, Parameters: parameters})
	}
	for _, value := range options.inputs {
		name, _, parseErr := parseNamedValue(value)
		if parseErr != nil {
			return conversion.Spec{}, fmt.Errorf("input: %w", parseErr)
		}
		if !validGraphAlias(name) {
			return conversion.Spec{}, fmt.Errorf("input: invalid name %q", name)
		}
		if aliasSeen[name] {
			return conversion.Spec{}, fmt.Errorf("input: name %q duplicates a filter alias", name)
		}
		aliasSeen[name] = true
		if spec.AuxInputs == nil {
			spec.AuxInputs = make(map[string]conversion.AuxInputSpec)
		}
		spec.AuxInputs[name] = conversion.AuxInputSpec{}
	}
	for _, value := range options.wires {
		left, source, parseErr := parseNamedValue(value)
		if parseErr != nil {
			return conversion.Spec{}, fmt.Errorf("wire: %w", parseErr)
		}
		separator := strings.LastIndex(left, ".")
		if separator <= 0 || separator == len(left)-1 {
			return conversion.Spec{}, fmt.Errorf("wire: invalid destination %q", left)
		}
		alias, port := left[:separator], left[separator+1:]
		sourceAlias, sourcePort, sourceErr := parseWireSource(source)
		if sourceErr != nil {
			return conversion.Spec{}, fmt.Errorf("wire: %w", sourceErr)
		}
		ref := conversion.PortRef{Alias: sourceAlias, Port: sourcePort}
		if alias == outputAlias {
			if port != "in" {
				return conversion.Spec{}, fmt.Errorf("wire: %s only has an \"in\" port", outputAlias)
			}
			if spec.Sink != nil {
				return conversion.Spec{}, fmt.Errorf("wire: output is already wired")
			}
			spec.Sink = &ref
			continue
		}
		index, ok := aliasIndex[alias]
		if !ok {
			return conversion.Spec{}, fmt.Errorf("wire: unknown filter alias %q", alias)
		}
		filter := &spec.Filters[index]
		if filter.Inputs == nil {
			filter.Inputs = make(map[string]conversion.PortRef)
		}
		if _, exists := filter.Inputs[port]; exists {
			return conversion.Spec{}, fmt.Errorf("wire: duplicate destination %s", left)
		}
		filter.Inputs[port] = ref
	}
	return spec, nil
}

// outputAlias is the reserved --wire destination for the pipeline's final
// output port (conversion.Spec.Sink), mirroring conversion.MainInputAlias
// ("@in") as a --wire source. Everything else about graph wiring — unknown
// aliases, cycles, ports left unconnected — is validated once, by
// conversion.Resolve/routing.NegotiateConversion, rather than duplicated
// here.
const outputAlias = "@out"

func validGraphAlias(value string) bool {
	if value == "" || value == outputAlias || value == conversion.MainInputAlias {
		return false
	}
	return !strings.ContainsAny(value, ".= \t\r\n")
}

func parseWireSource(value string) (string, string, error) {
	if value == "" {
		return "", "", fmt.Errorf("source must not be empty")
	}
	separator := strings.LastIndex(value, ".")
	if separator < 0 {
		return value, "out", nil
	}
	if separator == 0 || separator == len(value)-1 {
		return "", "", fmt.Errorf("invalid source %q", value)
	}
	return value[:separator], value[separator+1:], nil
}

// parseFilterSpec splits an optional "alias=" prefix off a filter
// specification before parsing the rest as a plugin spec. The alias's '='
// must be searched for only in the name region — up to whichever of '['
// or ':' comes first, or the end of the string — since a bracketed
// parameter segment (e.g. "mixer[in=2]") may itself contain '=' that must
// not be mistaken for the alias separator.
func parseFilterSpec(value string) (string, *conversion.PluginSpec, map[string]string, error) {
	nameEnd := len(value)
	if idx := strings.IndexByte(value, '['); idx >= 0 && idx < nameEnd {
		nameEnd = idx
	}
	if idx := strings.IndexByte(value, ':'); idx >= 0 && idx < nameEnd {
		nameEnd = idx
	}
	if equals := strings.IndexByte(value[:nameEnd], '='); equals >= 0 {
		alias := value[:equals]
		if alias == "" {
			return "", nil, nil, fmt.Errorf("filter alias must not be empty")
		}
		plugin, parameters, err := parsePluginSpec(value[equals+1:])
		return alias, plugin, parameters, err
	}
	plugin, parameters, err := parsePluginSpec(value)
	return "", plugin, parameters, err
}

func parseNamedValue(value string) (string, string, error) {
	equals := strings.IndexByte(value, '=')
	if equals <= 0 || equals == len(value)-1 {
		return "", "", fmt.Errorf("want NAME=VALUE")
	}
	return value[:equals], value[equals+1:], nil
}

// parsePluginSpec parses "name[param=value,...]:key=value,...". Parameters
// is nil unless a "[...]" segment was present; only filters currently
// accept one (see conversion.FilterSpec.Parameters), but the syntax is
// parsed uniformly here regardless of plugin role.
func parsePluginSpec(value string) (*conversion.PluginSpec, map[string]string, error) {
	if value == "" {
		return nil, nil, nil
	}
	spec, err := cliflag.ParseSpec(value)
	if err != nil {
		return nil, nil, err
	}
	return &conversion.PluginSpec{Name: spec.Name, Values: spec.Values}, spec.Parameters, nil
}

func rejectPluginParameters(role string, parameters map[string]string) error {
	if len(parameters) != 0 {
		return fmt.Errorf("%s does not accept parameters", role)
	}
	return nil
}

func inferMuxerName(outputs []catalog.OutputFormat, output string) (string, error) {
	extension := strings.ToLower(filepath.Ext(output))
	var match string
	for _, format := range outputs {
		if !slices.Contains(format.Extensions, extension) {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("multiple output formats match %q", extension)
		}
		match = format.Muxer
	}
	if match == "" {
		return "", fmt.Errorf("cannot infer output format from %q; use --format", output)
	}
	return match, nil
}

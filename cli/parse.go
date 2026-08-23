package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/internal/surface"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin/file"
)

var errHelp = errors.New("help requested")

// rawFields are the media properties a headerless PCM stream cannot state for
// itself. Each one names the raw reader config field the flag sets, so the CLI
// vocabulary and the component schema stay in step.
var rawFields = []struct{ flag, field, argument, help string }{
	{"raw-rate", "rate", "HZ", "raw PCM sample rate"},
	{"raw-coding", "coding", "CODING", "raw PCM sample coding (u8, s8, s16, s24, s32, f32, f64)"},
	{"raw-layout", "layout", "LAYOUT", "raw PCM channel layout (mono, stereo, 6ch, FL+FR+FC+LFE)"},
	{"raw-endian", "endian", "ENDIAN", "raw PCM byte order (little, big)"},
	{"raw-valid-bits", "validBits", "BITS", "raw PCM bits carrying information"},
}

var usage = buildUsage()

func buildUsage() string {
	var builder strings.Builder
	builder.WriteString(`usage: godec [options] INPUT OUTPUT

options:
  --plan                  inspect and print a Plan without creating OUTPUT
  --input-format EXT      explicitly select an input Format extension
`)
	for _, entry := range rawFields {
		option := "--" + entry.flag + " " + entry.argument
		builder.WriteString(fmt.Sprintf("  %-22s%s\n", option, entry.help))
	}
	return builder.String()
}

type invocation struct {
	input       string
	output      string
	planOnly    bool
	inputFormat string
	rawFields   map[string]string
}

func parse(args []string) (invocation, error) {
	result := invocation{rawFields: make(map[string]string)}
	set := flag.NewFlagSet("godec", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&result.planOnly, "plan", false, "")
	set.StringVar(&result.inputFormat, "input-format", "", "")
	values := make(map[string]*string, len(rawFields))
	fields := make(map[string]string, len(rawFields))
	for _, entry := range rawFields {
		values[entry.flag] = set.String(entry.flag, "", "")
		fields[entry.flag] = entry.field
	}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return invocation{}, errHelp
		}
		return invocation{}, err
	}
	if set.NArg() != 2 {
		return invocation{}, errors.New("exactly one INPUT and OUTPUT path are required")
	}
	result.input, result.output = set.Arg(0), set.Arg(1)
	set.Visit(func(item *flag.Flag) {
		if field, ok := fields[item.Name]; ok {
			result.rawFields[field] = *values[item.Name]
		}
	})
	return result, nil
}

func (i invocation) request() (job.Job, error) {
	selector, selected, err := i.inputSelector()
	if err != nil {
		return job.Job{}, err
	}
	inputReference, err := file.Reference(i.input)
	if err != nil {
		return job.Job{}, err
	}
	outputReference, err := file.Reference(i.output)
	if err != nil {
		return job.Job{}, err
	}
	var request job.Job
	if selected {
		request, err = surface.FileJob(i.input, inputReference, i.output, outputReference, &selector, nil)
	} else {
		request, err = surface.FileJob(i.input, inputReference, i.output, outputReference, nil, nil)
	}
	if err != nil {
		return job.Job{}, err
	}
	return request, nil
}

func (i invocation) inputSelector() (job.FormatSelector, bool, error) {
	name := i.inputFormat
	if name == "" && len(i.rawFields) != 0 {
		name = filepath.Ext(i.input)
		if name == "" {
			return job.FormatSelector{}, false, errors.New("raw properties for an extensionless input require --input-format")
		}
	}
	if name == "" {
		return job.FormatSelector{}, false, nil
	}
	extension, err := mediaformat.ParseExtension(name)
	if err != nil {
		return job.FormatSelector{}, false, err
	}
	selector, err := job.SelectFormatExtension(extension)
	if err != nil {
		return job.FormatSelector{}, false, err
	}
	if len(i.rawFields) != 0 {
		patch := config.NewPatch()
		for field, value := range i.rawFields {
			patch = patch.SetText(field, value)
		}
		selector = selector.WithConfig(patch)
	}
	return selector, true, nil
}

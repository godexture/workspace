package cli

import (
	"errors"
	"flag"
	"io"
	"path/filepath"

	"github.com/godexture/godec/config"
	"github.com/godexture/godec/diagnostic"
	"github.com/godexture/godec/internal/surface"
	"github.com/godexture/godec/job"
	mediaformat "github.com/godexture/godec/media/format"
	"github.com/godexture/godec/plugin/file"
)

var errHelp = errors.New("help requested")

const usage = `usage: godec [options] INPUT OUTPUT

options:
  --plan                  inspect and print a Plan without creating OUTPUT
  --input-format EXT      explicitly select an input Format extension
  --raw-rate HZ           raw PCM sample rate
  --raw-valid-bits BITS   raw PCM valid bits per sample
  --raw-layout LAYOUT     raw PCM channel layout
  --raw-endian ENDIAN     raw PCM byte order
`

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
	var rate, bits, layout, endian string
	set.StringVar(&rate, "raw-rate", "", "")
	set.StringVar(&bits, "raw-valid-bits", "", "")
	set.StringVar(&layout, "raw-layout", "", "")
	set.StringVar(&endian, "raw-endian", "", "")
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
	values := map[string]string{"rate": rate, "validBits": bits, "layout": layout, "endian": endian}
	set.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "raw-rate":
			result.rawFields["rate"] = values["rate"]
		case "raw-valid-bits":
			result.rawFields["validBits"] = values["validBits"]
		case "raw-layout":
			result.rawFields["layout"] = values["layout"]
		case "raw-endian":
			result.rawFields["endian"] = values["endian"]
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
	if err := file.ValidateDistinct(i.input, i.output); err != nil {
		for _, item := range diagnostic.ItemsOf(err) {
			if item.Code == "file.same-path" {
				return job.Job{}, err
			}
		}
		return job.Job{}, planningRequestError{cause: err}
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

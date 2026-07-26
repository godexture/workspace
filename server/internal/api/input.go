package api

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"

	"github.com/godexture/web/internal/jobs"
	"github.com/godexture/sdk/conversion"
	"github.com/labstack/echo/v4"
)

type inputReference struct {
	Kind     string `json:"kind"`
	PresetID string `json:"presetId,omitempty"`
}

type inputManifest struct {
	Main inputReference            `json:"main"`
	Aux  map[string]inputReference `json:"aux"`
}

func (s *Server) parseUpload(c echo.Context) error {
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, s.maxUploadBytes)
	if err := c.Request().ParseMultipartForm(32 << 20); err != nil {
		return conversion.NewError(conversion.CodePayloadTooLarge, "request body exceeds the server upload limit or is malformed: "+err.Error())
	}
	return nil
}

func parseSpec(c echo.Context) (conversion.Spec, error) {
	var spec conversion.Spec
	raw := c.FormValue("spec")
	if raw == "" {
		return spec, conversion.NewError(conversion.CodeInvalidSpec, "spec field is required")
	}
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return spec, conversion.NewError(conversion.CodeInvalidSpec, "invalid spec: "+err.Error())
	}
	return spec, nil
}

func parseInputs(c echo.Context, spec conversion.Spec) (inputManifest, error) {
	var inputs inputManifest
	raw := c.FormValue("inputs")
	if raw == "" {
		return inputs, conversion.NewError(conversion.CodeInvalidSpec, "inputs field is required")
	}
	if err := json.Unmarshal([]byte(raw), &inputs); err != nil {
		return inputs, conversion.NewError(conversion.CodeInvalidSpec, "invalid inputs: "+err.Error())
	}
	if inputs.Aux == nil {
		inputs.Aux = map[string]inputReference{}
	}
	if err := validateInputs(inputs, spec); err != nil {
		return inputs, err
	}
	return inputs, nil
}

func validateInputs(inputs inputManifest, spec conversion.Spec) error {
	if err := validateInputReference(inputs.Main); err != nil {
		return conversion.NewError(conversion.CodeInvalidSpec, "main input: "+err.Error())
	}
	if len(inputs.Aux) != len(spec.AuxInputs) {
		return conversion.NewError(conversion.CodeInvalidSpec, "auxiliary inputs do not match the conversion spec")
	}
	for name := range spec.AuxInputs {
		ref, ok := inputs.Aux[name]
		if !ok {
			return conversion.NewError(conversion.CodeInvalidSpec, "auxiliary input "+name+" is required")
		}
		if err := validateInputReference(ref); err != nil {
			return conversion.NewError(conversion.CodeInvalidSpec, "auxiliary input "+name+": "+err.Error())
		}
	}
	for name := range inputs.Aux {
		if _, ok := spec.AuxInputs[name]; !ok {
			return conversion.NewError(conversion.CodeInvalidSpec, "auxiliary input "+name+" is not declared by the conversion spec")
		}
	}
	return nil
}

func validateInputReference(input inputReference) error {
	switch input.Kind {
	case "file":
		if input.PresetID != "" {
			return fmt.Errorf("file input cannot specify a preset")
		}
	case "preset":
		if input.PresetID == "" {
			return fmt.Errorf("preset input requires presetId")
		}
	default:
		return fmt.Errorf("kind must be file or preset")
	}
	return nil
}

// openPreviewInputs opens all request-scoped inputs for pipeline negotiation.
func (s *Server) openPreviewInputs(c echo.Context, spec conversion.Spec) (conversion.InputSet, func(), error) {
	manifest, err := parseInputs(c, spec)
	if err != nil {
		return conversion.InputSet{}, nil, err
	}
	main, err := s.openInput(c, manifest.Main, "main")
	if err != nil {
		return conversion.InputSet{}, nil, err
	}
	closers := []io.Closer{main}
	aux := make(map[string]io.ReadSeeker, len(manifest.Aux))
	for _, name := range slices.Sorted(maps.Keys(manifest.Aux)) {
		input, openErr := s.openInput(c, manifest.Aux[name], "aux:"+name)
		if openErr != nil {
			closeInputs(closers)
			return conversion.InputSet{}, nil, openErr
		}
		closers = append(closers, input)
		aux[name] = input
	}
	return conversion.InputSet{Main: main, Aux: aux}, func() { closeInputs(closers) }, nil
}

func (s *Server) openInput(c echo.Context, input inputReference, field string) (io.ReadSeekCloser, error) {
	if input.Kind == "preset" {
		preset, ok := findPreset(input.PresetID)
		if !ok {
			return nil, notFoundPreset(input.PresetID)
		}
		return os.Open(s.presetPath(preset))
	}
	header, err := c.FormFile(field)
	if err != nil {
		return nil, conversion.NewError(conversion.CodeInvalidSpec, "file input "+field+" is required")
	}
	file, err := header.Open()
	if err != nil {
		return nil, err
	}
	return file, nil
}

// prepareJobInputs copies all uploaded files into the job store while preset
// paths stay shared. The caller owns and must clean up returned owned paths
// until Store.Start takes over.
func (s *Server) prepareJobInputs(c echo.Context, spec conversion.Spec) (jobs.Inputs, error) {
	manifest, err := parseInputs(c, spec)
	if err != nil {
		return jobs.Inputs{}, err
	}
	main, err := s.prepareInput(c, manifest.Main, "main")
	if err != nil {
		return jobs.Inputs{}, err
	}
	inputs := jobs.Inputs{Main: main, Aux: make(map[string]jobs.Input, len(manifest.Aux))}
	for _, name := range slices.Sorted(maps.Keys(manifest.Aux)) {
		input, prepareErr := s.prepareInput(c, manifest.Aux[name], "aux:"+name)
		if prepareErr != nil {
			removeOwnedInputs(inputs)
			return jobs.Inputs{}, prepareErr
		}
		inputs.Aux[name] = input
	}
	return inputs, nil
}

func (s *Server) prepareInput(c echo.Context, input inputReference, field string) (jobs.Input, error) {
	if input.Kind == "preset" {
		preset, ok := findPreset(input.PresetID)
		if !ok {
			return jobs.Input{}, notFoundPreset(input.PresetID)
		}
		return jobs.Input{Path: s.presetPath(preset)}, nil
	}
	header, err := c.FormFile(field)
	if err != nil {
		return jobs.Input{}, conversion.NewError(conversion.CodeInvalidSpec, "file input "+field+" is required")
	}
	file, err := header.Open()
	if err != nil {
		return jobs.Input{}, err
	}
	defer file.Close()

	dest, err := s.jobs.CreateInputFile()
	if err != nil {
		return jobs.Input{}, err
	}
	path := dest.Name()
	defer dest.Close()
	if _, err := io.Copy(dest, file); err != nil {
		_ = os.Remove(path)
		return jobs.Input{}, err
	}
	return jobs.Input{Path: path, Owned: true}, nil
}

func removeOwnedInputs(inputs jobs.Inputs) {
	if inputs.Main.Owned {
		_ = os.Remove(inputs.Main.Path)
	}
	for _, input := range inputs.Aux {
		if input.Owned {
			_ = os.Remove(input.Path)
		}
	}
}

func closeInputs(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}

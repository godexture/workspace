package host

import (
	"strings"

	"github.com/godexture/godec/diagnostic"
)

// RunOption configures one execution without mutating its Host or Prepared
// plan. The same option values are accepted by Host.Run and Prepared.Run.
type RunOption func(*runOptions)

// ObservationOption configures the bounded consumers of one observation hub.
type ObservationOption func(*observationOptions)

type observationOptions struct {
	mode        Observation
	history     int
	historySet  bool
	delivery    int
	deliverySet bool
	sink        EventSink
	invalid     []string
}

type runOptions struct {
	observation    observationOptions
	observationSet bool
	invalid        []string
}

// Observe enables one per-Run event hub. At least one bounded history or live
// delivery option is required.
func Observe(mode Observation, values ...ObservationOption) RunOption {
	configuration := observationOptions{mode: mode}
	for _, option := range values {
		if option != nil {
			option(&configuration)
		}
	}
	configuration.invalid = append([]string(nil), configuration.invalid...)
	return func(options *runOptions) {
		if options.observationSet {
			options.invalid = append(options.invalid, "observation")
			return
		}
		options.observationSet = true
		options.observation = configuration
	}
}

// RetainEvents keeps the newest limit events in Result.Events.
func RetainEvents(limit int) ObservationOption {
	return func(options *observationOptions) {
		if options.historySet {
			options.invalid = append(options.invalid, "history")
			return
		}
		options.historySet = true
		options.history = limit
	}
}

// DeliverEvents sends events through a non-blocking queue of the supplied
// capacity. Overflow is counted instead of applying media-path backpressure.
func DeliverEvents(capacity int, sink EventSink) ObservationOption {
	return func(options *observationOptions) {
		if options.deliverySet {
			options.invalid = append(options.invalid, "delivery")
			return
		}
		options.deliverySet = true
		options.delivery = capacity
		options.sink = sink
	}
}

func resolveRunOptions(values []RunOption) (runOptions, error) {
	result := runOptions{}
	for _, option := range values {
		if option != nil {
			option(&result)
		}
	}
	if !result.observationSet && len(result.invalid) == 0 {
		return result, nil
	}
	configuration := result.observation
	invalid := append([]string(nil), result.invalid...)
	invalid = append(invalid, configuration.invalid...)
	if !configuration.mode.Valid() || configuration.mode == ObservationOff {
		invalid = append(invalid, "mode")
	}
	if configuration.historySet && configuration.history <= 0 {
		invalid = append(invalid, "history")
	}
	if configuration.deliverySet && (configuration.delivery <= 0 || configuration.sink == nil) {
		invalid = append(invalid, "delivery")
	}
	if !configuration.historySet && !configuration.deliverySet {
		invalid = append(invalid, "consumer")
	}
	if len(invalid) != 0 {
		return runOptions{}, diagnostic.NewError(diagnostic.NewItem(
			"host.observation-option", diagnostic.ErrorSeverity, diagnostic.Path{}, "Run observation configuration is invalid",
			map[string]string{"fields": strings.Join(invalid, ",")},
		))
	}
	return result, nil
}

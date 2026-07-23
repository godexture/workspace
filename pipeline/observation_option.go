package pipeline

type buildConfig struct {
	observation ObservationMode
}

type BuildOption func(*buildConfig)

func WithObservation(mode ObservationMode) BuildOption {
	return func(config *buildConfig) {
		config.observation = mode
	}
}

package media

type Frame interface {
	Retainer
	Pts() Pts
}

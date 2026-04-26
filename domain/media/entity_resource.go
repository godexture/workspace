package media

type Retainer interface {
	Retain()
	Release()
}

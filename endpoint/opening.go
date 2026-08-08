package endpoint

// Direction is the direction of one opened endpoint session.
type Direction uint8

const (
	SourceDirection Direction = iota + 1
	SinkDirection
)

func (d Direction) Valid() bool { return d == SourceDirection || d == SinkDirection }

// Opening is the node-local endpoint contract selected during planning.
type Opening struct {
	direction Direction
	trait     Trait
}

func NewOpening(direction Direction, trait Trait) (Opening, error) {
	if !direction.Valid() || !trait.Valid() {
		return Opening{}, ErrInvalidTrait
	}
	return Opening{direction: direction, trait: trait}, nil
}

func (o Opening) Valid() bool          { return o.direction.Valid() && o.trait.Valid() }
func (o Opening) Direction() Direction { return o.direction }
func (o Opening) Trait() Trait         { return o.trait }

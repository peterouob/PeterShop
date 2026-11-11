package circuit

type Permit[T any] struct {
	g     *Gate[T]
	epoch uint64
	done  bool
}

func (p *Permit[T]) MarkUsed() { p.done = true }

func (p *Permit[T]) Release() {
	if p.done || p.g == nil {
		return
	}

	p.done = true
	p.g.release(p.epoch)
}

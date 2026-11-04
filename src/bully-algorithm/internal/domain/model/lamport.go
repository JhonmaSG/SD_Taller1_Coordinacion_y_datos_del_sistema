package model

// LamportTime representa un timestamp del reloj lógico de Lamport.
type LamportTime uint64

// LamportClock encapsula el estado mutable de un reloj lógico.
type LamportClock struct {
	value LamportTime
}

// NewLamportClock crea un reloj inicializado con el valor indicado.
func NewLamportClock(initial LamportTime) LamportClock {
	return LamportClock{value: initial}
}

// Value devuelve el valor actual del reloj.
func (c LamportClock) Value() LamportTime {
	return c.value
}

// Tick avanza el reloj antes de crear un evento local.
func (c *LamportClock) Tick() LamportTime {
	if c.value < ^LamportTime(0) {
		c.value++
	}

	return c.value
}

// Observe incorpora un timestamp remoto y avanza el reloj para el evento local
// que procesa ese mensaje.
func (c *LamportClock) Observe(remote LamportTime) LamportTime {
	if remote > c.value {
		c.value = remote
	}

	return c.Tick()
}

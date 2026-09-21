package core

import "github.com/miferdev/superComparator/internal/chain"

// Event es cualquier aviso tipado que el núcleo envía a la interfaz
// (la TUI los pinta; el modo CLI los resume). Las funciones que los reciben
// deben ser seguras para uso concurrente.
type Event interface{ event() }

type RunStarted struct {
	Phase string
	Total int
}

type ItemStarted struct {
	Item  string
	Index int
	Total int
}

type ChainResolved struct {
	Item         string
	Chain        string
	Product      chain.Product
	Score        float64
	Alternatives []Alternative
}

type ItemFailed struct {
	Item  string
	Chain string
	Err   string
}

type ItemNeedsReview struct {
	Item string
}

type PriceChanged struct {
	Item  string
	Chain string
	Old   float64
	New   float64
}

type ProductDelisted struct {
	Item  string
	Chain string
}

type RunFinished struct {
	Phase    string
	Resolved int
	Review   int
	Failed   int
	Err      error
}

type Alternative struct {
	URL          string
	Name         string
	Score        float64
	Price        float64
	MeasurePrice float64
	MeasureUnit  string
}

func (RunStarted) event()      {}
func (ItemStarted) event()     {}
func (ChainResolved) event()   {}
func (ItemFailed) event()      {}
func (ItemNeedsReview) event() {}
func (PriceChanged) event()    {}
func (ProductDelisted) event() {}
func (RunFinished) event()     {}

package store

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miferdev/superComparator/internal/chain"
)

// Límites del bucle que no son configuración de una tienda.
const (
	// reclaimTTL es lo que puede llevar una ficha en 'descargando' antes de
	// darse por atascada: si el proceso muere, al siguiente arranque la cola
	// vuelve a avanzar en lugar de quedarse esperando.
	reclaimTTL = time.Minute
	// defaultBatch son las fichas que coge una cadena en cada pasada si quien
	// llama no dice cuántas.
	defaultBatch = 50
)

// Fallos que no son de la web: la ficha vino pero no sirve, o ya no tiene
// sentido reintentarla. Se cuentan como fallidas y suben los intentos, pero la
// pasada sigue y no se inventa ningún precio.
var (
	errSinPrecio    = errors.New("la ficha no trae precio")
	errNoDisponible = errors.New("la ficha ya no está disponible")
	errIntentos     = errors.New("agotados los intentos")
)

// PriceFetcher descarga la ficha de un producto. Lo implementa el catálogo
// (que ya tiene los adaptadores de las tiendas).
type PriceFetcher interface {
	Fetch(ctx context.Context, chainID, url string) (chain.Product, error)
}

// PriceJobResult resume una pasada del bucle.
type PriceJobResult struct {
	Hechos   int
	Fallidos int
	Duracion time.Duration
}

// PriceLoop consume la cola de precios. Solo toca cadenas con PreciosActivos
// (Alcampo los tiene a 0 y su WAF devuelve 403).
//
// El ritmo y la concurrencia salen de cada cadena, no de aquí: Sleep y Now son
// inyectables para que los tests no dependan del reloj ni tengan que esperar.
type PriceLoop struct {
	store       *Store
	fetch       PriceFetcher
	chains      []Chain
	MaxIntentos int
	Sleep       func(time.Duration)
	Now         func() time.Time

	// OnFicha recibe el producto recién descargado para que catalog guarde el
	// nombre y la categoría definitivos. Es opcional: si no se pone, la ficha
	// solo sirve para el precio.
	OnFicha func(productID int64, p chain.Product)

	log *slog.Logger
	mu  sync.Mutex
	// reclaimed evita rescatar más de una vez, y ritmo lleva el próximo hueco
	// de cada flujo de cada cadena (mu los cubre a los dos).
	reclaimed bool
	ritmo     ritmo
}

// NewPriceLoop arma el bucle. Las cadenas se leen de la tabla chains, que es
// donde vive el ritmo de cada tienda y si sus precios están activos: así el
// usuario ajusta el ritmo sin tocar código ni recompilar. Si se le pasan
// cadenas, se usan esas.
func NewPriceLoop(st *Store, f PriceFetcher, chains []Chain) *PriceLoop {
	return &PriceLoop{
		store:       st,
		fetch:       f,
		chains:      chains,
		MaxIntentos: MaxIntentos,
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// WithOnFicha pone el enganche que guarda el nombre y la categoría definitivos
// de cada ficha descargada. El nombre se normaliza en catalog, no aquí, porque
// es quien usa match.
func (l *PriceLoop) WithOnFicha(f func(productID int64, p chain.Product)) *PriceLoop {
	l.OnFicha = f
	return l
}

// WithLogger pone un logger donde avisar de los fallos que no cortan la pasada.
// Sin logger se descartan; hay que llamarlo antes de arrancar el bucle.
func (l *PriceLoop) WithLogger(log *slog.Logger) *PriceLoop {
	if log != nil {
		l.log = log
	}
	return l
}

func (l *PriceLoop) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *PriceLoop) sleep(d time.Duration) {
	if l.Sleep != nil {
		l.Sleep(d)
		return
	}
	time.Sleep(d)
}

func (l *PriceLoop) maxIntentos() int {
	if l.MaxIntentos > 0 {
		return l.MaxIntentos
	}
	return MaxIntentos
}

// Run una pasada: coge hasta limit fichas pendientes por cadena activa,
// respeta el ritmo de cada una y guarda el precio. Devuelve el resumen.
func (l *PriceLoop) Run(ctx context.Context, limit int) (PriceJobResult, error) {
	if limit <= 0 {
		limit = defaultBatch
	}
	inicio := l.now()
	var res contador

	// Las fichas que dejó en 'descargando' un proceso muerto se recuperan una
	// sola vez, antes de empezar a repartir trabajo.
	if err := l.reclaim(); err != nil {
		return PriceJobResult{}, err
	}

	// Las cuentas por cadena las lleva este goroutine y nadie más: solo él
	// reserva, y así es él quien decide el round-robin.
	porCadena := map[string]int{}
	for {
		if err := ctx.Err(); err != nil {
			return res.resumen(l.now().Sub(inicio)), err
		}
		var (
			wg    sync.WaitGroup
			turno bool
		)
		for _, c := range l.activas() {
			pedir := c.Concurrencia
			if pedir <= 0 {
				pedir = 1
			}
			if quedan := limit - porCadena[c.ID]; pedir > quedan {
				pedir = quedan
			}
			if pedir <= 0 {
				continue
			}
			entradas, err := l.store.NextQueuedChain(l.now(), c.ID, pedir)
			if err != nil {
				wg.Wait()
				return res.resumen(l.now().Sub(inicio)), err
			}
			if len(entradas) == 0 {
				continue
			}
			turno = true
			porCadena[c.ID] += len(entradas)
			wg.Add(1)
			go func(c Chain, entradas []QueueEntry) {
				defer wg.Done()
				l.trabajar(ctx, c, entradas, &res)
			}(c, entradas)
		}
		wg.Wait()
		// Sin turno no hay nada pendiente ni reintentable ahora mismo.
		if !turno {
			break
		}
	}
	return res.resumen(l.now().Sub(inicio)), nil
}

// RunUntilEmpty repite pasadas hasta que no queda nada pendiente, respetando
// ctx. Úsalo desde el servidor, en segundo plano.
func (l *PriceLoop) RunUntilEmpty(ctx context.Context) (PriceJobResult, error) {
	inicio := l.now()
	var total PriceJobResult
	for {
		if err := ctx.Err(); err != nil {
			total.Duracion = l.now().Sub(inicio)
			return total, err
		}
		pasada, err := l.Run(ctx, 0)
		total.Hechos += pasada.Hechos
		total.Fallidos += pasada.Fallidos
		if err != nil {
			total.Duracion = l.now().Sub(inicio)
			return total, err
		}
		// Una pasada sin nada hecho ni fallado significa que lo que queda espera
		// su turno de reintento; sin esta salida no acabaría nunca.
		if pasada.Hechos == 0 && pasada.Fallidos == 0 {
			break
		}
	}
	total.Duracion = l.now().Sub(inicio)
	return total, nil
}

// activas devuelve las cadenas con las que se puede trabajar: las que se le
// pasaron y tienen los precios activos.
//
// La lista se la da quien arma el bucle, y tiene que ser exactamente las cadenas
// para las que hay adaptador: si el bucle preguntara por una tienda que no
// conoce, leería la cola, pediría esa ficha y la marcaría como fallida ("cadena
// desconocida") sin motivo. Por eso no se recorre la base entera aquí, aunque las
// inactive, el ritmo y la concurrencia sí salen de la tabla chains.
func (l *PriceLoop) activas() []Chain {
	// Las cadenas con los precios apagados se quedan fuera: sus fichas se quedan
	// en la cola, que es justo donde deben estar.
	out := make([]Chain, 0, len(l.chains))
	for _, c := range l.chains {
		if !c.PreciosActivos {
			continue
		}
		out = append(out, c)
	}
	return out
}

// reclaim rescata una sola vez las fichas que dejó a medias un proceso muerto.
// Si falla no marca nada como hecho y la pasada siguiente lo intenta otra vez.
func (l *PriceLoop) reclaim() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reclaimed {
		return nil
	}
	rescatadas, err := l.store.ReclaimStale(reclaimTTL)
	if err != nil {
		return err
	}
	l.reclaimed = true
	if rescatadas > 0 {
		l.log.Warn("fichas de precio rescatadas de descargas a medias", "n", rescatadas)
	}
	return nil
}

// trabajar descarga las fichas que se le han reservado a una cadena, con la
// concurrencia que dice su configuración. Cada worker lleva su propio hueco: así
// la pausa se respeta entre descargas de la MISMA cadena (también entre
// tandas, no solo dentro de una) y dos cadenas distintas van en paralelo,
// porque cada una duerme en su goroutine y no bloquea a nadie.
func (l *PriceLoop) trabajar(ctx context.Context, c Chain, entradas []QueueEntry, res *contador) {
	flujos := c.Concurrencia
	if flujos <= 0 {
		flujos = 1
	}
	if flujos > len(entradas) {
		flujos = len(entradas)
	}
	in := make(chan QueueEntry)
	var wg sync.WaitGroup
	for i := 0; i < flujos; i++ {
		wg.Add(1)
		go func(flujo int) {
			defer wg.Done()
			for e := range in {
				if ctx.Err() != nil {
					return
				}
				if d := l.ritmo.toma(c, flujo, l.now()); d > 0 {
					l.sleep(d)
					if ctx.Err() != nil {
						return
					}
				}
				l.procesar(ctx, c, e, res)
			}
		}(i)
	}
	// Al cancelar el contexto las fichas que no llegan a entrar se quedan en
	// 'descargando'; ReclaimStale las recupera en el siguiente arranque.
	for _, e := range entradas {
		select {
		case in <- e:
		case <-ctx.Done():
		}
	}
	close(in)
	wg.Wait()
}

// procesar descarga una ficha y guarda lo que venga. Lo único que escribe del
// producto es el precio: el nombre y la categoría son del indexado.
func (l *PriceLoop) procesar(ctx context.Context, c Chain, e QueueEntry, res *contador) {
	max := l.maxIntentos()
	// Ya no se reintenta sola (FailQueue la dejó en 'error'); si alguien la
	// dejó pendiente a mano, se aparca otra vez en vez de descargarse.
	if e.Attempts >= max {
		res.sumar(0, 1)
		l.fallar(e, errIntentos)
		return
	}
	p, err := l.fetch.Fetch(ctx, c.ID, e.URL)
	if err != nil {
		res.sumar(0, 1)
		l.fallar(e, err)
		return
	}
	if !p.Available {
		res.sumar(0, 1)
		l.fallar(e, errNoDisponible)
		return
	}
	// Un producto vendido al peso no trae precio de unidad (viene a 0 a
	// propósito) y su único dato es el €/kg. Cuenta como precio guardado.
	if p.Price <= 0 && !(p.PrecioEsPorMedida() && p.MeasurePrice > 0) {
		res.sumar(0, 1)
		l.fallar(e, errSinPrecio)
		return
	}
	// El nombre y la categoría definitivos vienen de la ficha, no del sitemap:
	// sobre todo para DÍÁ, cuyo sitemap solo trae la categoría. Quien normaliza
	// el nombre para poder buscarlo es catalog (que usa match), no store, así
	// que se lo dejamos a él con OnFicha.
	if l.OnFicha != nil {
		l.OnFicha(e.ProductID, p)
	}
	if err := l.store.SetPrice(e.ProductID, p.Price, priceBasis(p), p.MeasurePrice,
		p.MeasureUnit, p.Available); err != nil {
		res.sumar(0, 1)
		l.fallar(e, err)
		return
	}
	if err := l.store.MarkQueueDone(e.ProductID); err != nil {
		res.sumar(0, 1)
		l.fallar(e, err)
		return
	}
	res.sumar(1, 0)
}

// fallar deja la ficha para otro intento con la espera del backoff. Ni un 404 ni
// los intentos ya agotados se arreglan reintentando: esos se aparcan de una en
// 'error' y esperan a que alguien rearme la ficha.
func (l *PriceLoop) fallar(e QueueEntry, err error) {
	intentos := e.Attempts + 1
	switch {
	case errors.Is(err, chain.ErrNotFound), errors.Is(err, errIntentos):
		intentos = l.maxIntentos()
	}
	siguiente := l.now().Add(l.backoff(intentos))
	if _, ferr := l.store.FailQueue(e.ProductID, intentos, siguiente, err.Error()); ferr != nil {
		// La ficha se queda en 'descargando' y ReclaimStale la recuperará al
		// siguiente arranque; no hay más que hacer aquí sin romper la pasada.
		l.log.Warn("no se pudo anotar el fallo de la ficha", "product", e.ProductID, "err", ferr)
	}
}

// contador lleva las cuentas de la pasada desde varios workers a la vez.
type contador struct {
	hechos   atomic.Int64
	fallidos atomic.Int64
}

func (c *contador) sumar(hechos, fallidos int) {
	if hechos > 0 {
		c.hechos.Add(int64(hechos))
	}
	if fallidos > 0 {
		c.fallidos.Add(int64(fallidos))
	}
}

func (c *contador) resumen(d time.Duration) PriceJobResult {
	return PriceJobResult{
		Hechos:   int(c.hechos.Load()),
		Fallidos: int(c.fallidos.Load()),
		Duracion: d,
	}
}

// Stats resume las fichas de una cadena por estado de la cola. Lo usa la
// pantalla de estado para decir cuántos productos quedan por poner precio.
func (l *PriceLoop) Stats(chainID string) (map[string]int, error) {
	return l.store.QueueStats(chainID)
}
